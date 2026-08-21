# TEMPORAL

## Overview

This document describes the architecture change for bulk file archive downloads — migrating from a **synchronous gRPC-streaming approach** to an **asynchronous Temporal workflow** with feature-flagged rollback capability.

---

## Before: Synchronous gRPC Streaming

### Architecture Flow

```graph
┌─────────────┐     POST /api/files/download-many  ┌──────────────┐
│   Client    │ ─────────────────────────────────> │ File Server  │
│  (Browser)  │                                    │   (HTTP)     │
└─────────────┘                                    └──────┬───────┘
                                                          │
                                    gRPC stream DownloadArchive
                                                          │
                                                          ▼
                                                   ┌──────────────┐
                                                   │   Storage    │
                                                   │   Service    │
                                                   │   (gRPC)     │
                                                   └──────┬───────┘
                                                          │
                                    Fetch each file from storage
                                    Zip on-the-fly
                                    Stream bytes back
                                                          │
                                                          ▼
                                    ┌─────────────────────────────┐
                                    │  Response: application/zip  │
                                    │  (streamed chunk by chunk)  │
                                    └─────────────────────────────┘
```

### Request Lifecycle

1. **Client** sends `POST /api/files/download-many` with `{\"file_ids\": [\"id1\", \"id2\"]}`
2. **File Server** calls `StorageService.DownloadArchive()` via gRPC streaming
3. **Storage Service** fetches each file from S3/Local, zips them into a temp file
4. **Storage Service** streams the zip bytes back through the gRPC connection
5. **File Server** proxies those bytes to the HTTP response
6. **Client** receives a synchronous `application/zip` download

### Problems

| Issue                      | Impact                                                                              |
| -------------------------- | ----------------------------------------------------------------------------------- |
| **Long-lived gRPC stream** | Connection held open for minutes; vulnerable to network hiccups                     |
| **No retry granularity**   | If file 9 of 10 fails, the entire archive fails; no resume                          |
| **No progress visibility** | Client sees a spinner; no idea if it's 10% or 90% done                              |
| **Proxying bytes**         | Every archive byte flows: S3 → Storage Service → gRPC → File Server → HTTP → Client |
| **Memory pressure**        | Storage Service holds the entire zip in a temp file; File Server buffers the stream |
| **Tight coupling**         | File Server cannot function if Storage Service is busy or down                      |

---

## After: Asynchronous Temporal Workflow

### Architecture Flow

```graph
┌─────────────┐     POST /api/files/archives      ┌──────────────┐
│   Client    │ ─────────────────────────────────> │ File Server  │
│  (Browser)  │                                    │   (HTTP)     │
└─────────────┘                                    └──────┬───────┘
       │                                                  │
       │ 202 Accepted                                     │ Start Workflow
       │ {\"workflow_id\": \"...\",                       │
       │  \"status_url\": \"...\"}                        ▼
       │                                            ┌──────────────┐
       │                                            │   Temporal   │
       │                                            │   Server     │
       │                                            └──────┬───────┘
       │                                                   │
       │                    ┌──────────────────────────────┘
       │                    │ Task Queue: \"archive-queue\"
       │                    ▼
       │            ┌──────────────┐
       │            │ File Server  │
       │            │   Worker     │
       │            │ (separate    │
       │            │  container)  │
       │            └──────┬───────┘
       │                   │
       │    ┌──────────────┼──────────────┐
       │    ▼              ▼              ▼
       │  Activity 1    Activity 2    Activity 3
       │  Resolve       Download      Zip
       │  URLs          (parallel)    Files
       │    │              │            │
       │    ▼              ▼            ▼
       │  gRPC          HTTP GET      Local FS
       │  metadata      presigned     /tmp/...
       │    │           URL → S3        │
       │    │              │            │
       │    └──────────────┴────────────┘
       │                   │
       │              Activity 4
       │              Upload Archive
       │                   │
       │                   ▼
       │              gRPC stream
       │              → Storage Service
       │                   │
       │                   ▼
       │              S3 / Local
       │
       │ GET /api/files/archives/{id}/status
       │ <─────────────────────────────────────
       │ {\"status\": \"processing\"}
       │
       │ ... later ...
       │
       │ GET /api/files/archives/{id}/status
       │ <─────────────────────────────────────
       │ {\"status\": \"completed\",
       │  \"archive_id\": \"uuid\"}
```

### Request Lifecycle

1. **Client** sends `POST /api/files/archives` with `{\"file_ids\": [...]}`
2. **File Server** starts a Temporal workflow (`BulkDownloadWorkflow`) and immediately returns:
   - `202 Accepted`
   - `workflow_id` and `status_url` for polling
3. **Temporal Server** queues the workflow task
4. **File Server Worker** picks up the task and executes activities:
   - **Activity 1: ResolveFiles** — gRPC call to Storage Service `GetDownloadURLs` to get presigned S3 URLs
   - **Activity 2: DownloadFile** (parallel) — HTTP GET each file directly from S3/MinIO using presigned URLs
   - **Activity 3: ZipFiles** — Create a local zip from downloaded temp files
   - **Activity 4: UploadArchive** — Stream the final zip back to Storage Service via gRPC `UploadFile`
   - **Activity 5: Cleanup** — Delete temp files (best effort)
5. **Client** polls `GET /api/files/archives/{id}/status` until `status: completed`
6. **Client** uses the returned `archive_id` to download the completed archive via the normal file download endpoint

### Activity Breakdown

| Activity                | Input                                   | Output                            | Network                                        | Retryable         |
| ----------------------- | --------------------------------------- | --------------------------------- | ---------------------------------------------- | ----------------- |
| `ResolveFilesActivity`  | `[]string` file IDs                     | `[]ResolvedFile` (presigned URLs) | gRPC to Storage Service (lightweight metadata) | ✅ Yes            |
| `DownloadFileActivity`  | Presigned URL, temp path                | File on local disk                | HTTP GET directly to S3/MinIO                  | ✅ Yes (per file) |
| `ZipFilesActivity`      | `[]ResolvedFile`, temp dir, output path | Zip file on disk                  | Local filesystem only                          | ✅ Yes            |
| `UploadArchiveActivity` | Zip path, archive name                  | Archive file ID                   | gRPC stream to Storage Service                 | ✅ Yes            |
| `CleanupActivity`       | Temp directory                          | —                                 | Local filesystem                               | ✅ Best effort    |

### Why This Is Better

| Concern            | Before                                               | After                                                              |
| ------------------ | ---------------------------------------------------- | ------------------------------------------------------------------ |
| **Reliability**    | All-or-nothing; single failure kills entire download | Per-activity retries; file 7 failing doesn't restart file 1-6      |
| **Observability**  | Black box spinner                                    | Pollable status; Temporal UI shows every activity state            |
| **Resource usage** | File Server holds HTTP connection open for minutes   | Immediate 202 response; heavy lifting in background worker         |
| **Scalability**    | Storage Service does zipping + streaming             | Workers scale independently; direct S3 downloads bypass gRPC proxy |
| **Decoupling**     | File Server blocks on Storage Service                | File Server only starts workflows; workers handle execution        |

---

## Migration: Feature Flag

The migration uses a runtime feature flag `USE_TEMPORAL_ARCHIVE` — no code redeploy needed to rollback.

### Flag Behavior

| `USE_TEMPORAL_ARCHIVE` | Behavior                                                                               | Risk                                          |
| ---------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------- |
| `false` (default)      | Old sync path: `FileHandler` → `FileService.DownloadMultiple` → gRPC `DownloadArchive` | Zero — this is the existing production path   |
| `true`                 | New async path: `FileHandler` → Temporal `ExecuteWorkflow` → 202 Accepted              | Canary-tested; flip back to `false` instantly |

### Rollback Procedure

```bash
# If Temporal path has issues, revert in 10 seconds:
USE_TEMPORAL_ARCHIVE=false docker compose up -d file-server
```

No image rebuild. No database migration. Just an env var restart.

---

## Proto Versioning Strategy

The Storage Service proto follows **additive-only** changes:

- `DownloadArchive` is **kept but marked deprecated** (`option deprecated = true`)
- `GetDownloadURLs` is **added** as the new canonical method
- Old clients (v1) continue to work
- New clients (v2 / Temporal worker) use `GetDownloadURLs`

After 100% migration and burn-in period, `DownloadArchive` can return `UNIMPLEMENTED` but remains in the proto to avoid breaking unknown clients.

---

## Component Responsibilities

| Component                | Before                              | After                                                       |
| ------------------------ | ----------------------------------- | ----------------------------------------------------------- |
| **File Server (HTTP)**   | Proxies archive bytes synchronously | Starts workflows, returns 202, handles status polling       |
| **File Server (Worker)** | Did not exist                       | Runs Temporal activities: download, zip, upload             |
| **Storage Service**      | Stores, fetches, **and zips** files | Stores, fetches, and **generates presigned URLs**           |
| **Temporal Server**      | Did not exist                       | Queues tasks, tracks workflow state, handles retries        |
| **S3 / MinIO**           | Accessed only by Storage Service    | Accessed directly by File Server workers via presigned URLs |

---

## Data Flow Comparison

### Before (Synchronous)

```
Client ──HTTP──> File Server ──gRPC stream──> Storage Service ──S3──> Files
       <──────────────────── zip bytes ─────────────────────────────
```

### After (Asynchronous)

```
Client ──HTTP──> File Server ──Temporal──> Worker ──HTTP──> S3 (direct download)
       <──202──┘                              │
                                              ├──local FS──> Zip
                                              │
                                              └──gRPC──> Storage Service ──S3 (upload)

Client ──HTTP poll──> File Server <──Temporal query──> Workflow state
```

---

## Files Added / Modified

### New Files

- `services/file-server/internal/temporal/workflow.go`
- `services/file-server/internal/temporal/activities.go`
- `services/file-server/internal/temporal/types.go`
- `services/file-server/cmd/worker/main.go`
- `services/file-server/Dockerfile.worker`
- `services/storage/internal/server/grpc_v2.go` (v2 proto methods)

### Modified Files

- `services/storage/proto/storage/v2/storage.proto` (added `GetDownloadURLs`, deprecated `DownloadArchive`)
- `services/storage/internal/server/grpc.go` (registered v2 server)
- `services/storage/internal/service/file.go` (added `GetByIDs`, `PresignFetch`)
- `services/storage/internal/storage/provider.go` (added `PresignFetch` to interface)
- `services/file-server/internal/client/client.go` (added `GetDownloadURLs`)
- `services/file-server/internal/handler/file.go` (feature flag in `DownloadMultiple`)
- `services/file-server/internal/config/config.go` (added `UseTemporalArchive`, `TemporalHost`, `TemporalQueue`)
- `docker-compose.yml` (added `temporal`, `temporal-ui`, `file-server-worker`)
- `.env` (added feature flag and Temporal config)
  """

with open('/mnt/agents/output/temporal-archive-architecture.md', 'w', encoding='utf-8') as f:
f.write(content)

print("Saved")
