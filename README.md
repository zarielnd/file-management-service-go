# go-file-service — System Description

## Overview

A file storage service.

Users upload a single file and receive an ID. They can later retrieve the file by that ID. One file in, one file out.

## Architecture (final state)

```graph
[Frontend] → HTTP → [File Server] → gRPC → [Storage Service]
                          ↓
                   [Temporal Worker]
```

## Phases

### Phase 1 — Go Fundamentals + Web Server

Build a single Go binary that handles file upload and retrieval over HTTP. Learn core Go concepts along the way (types, structs, interfaces, error handling, file I/O, packages/modules).

**Deliverable:** `POST /files` (upload, returns ID), `GET /files/{id}` (download by ID), `GET /health`. Simple UI. Runnable via `go run`.

### Phase 2 — Backend Service + gRPC + Postgres

Split into two services: HTTP server (external) and storage service (internal, gRPC). Add Postgres for file metadata. Add unit tests.

**Deliverable:** Two services running together. HTTP → gRPC → Postgres + file storage. Unit tests passing. `docker-compose` for Postgres.

### Phase 3 — Temporal

Orchestrate the upload flow with a Temporal workflow (validate → store → record metadata). Add cleanup workflow for orphaned files.

**Deliverable:** Temporal worker as a separate binary. `docker-compose` includes Temporal server + UI. Upload flow orchestrated by Temporal.

### Phase 3.5 (TBD) — ConnectRPC

Migrate from standard gRPC to ConnectRPC.

### Phase 4 — Deployment + CI + Integration Tests

Dockerize, set up CI pipeline, add integration tests.

**Deliverable:** CI green. Full stack runs via `docker-compose up`. Integration tests pass.
