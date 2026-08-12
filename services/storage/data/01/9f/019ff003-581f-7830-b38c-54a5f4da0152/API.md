# File Management Service API

## Overview

The File Management Service exposes an HTTP API for uploading, listing, downloading, and retrieving metadata for files.

The HTTP API is provided by the **File Server**. File persistence is handled internally by the **Storage Service** through gRPC.

```text
[Frontend]
     │
     │ HTTP
     ▼
[File Server]
     │
     │ gRPC
     ▼
[Storage Service]
     │
     ▼
[Local Filesystem]
```

## Base URL

```text
http://localhost:8080
```

## Endpoints

| Method | Endpoint               | Description                      |
| ------ | ---------------------- | -------------------------------- |
| `POST` | `/files`               | Upload one or more files         |
| `GET`  | `/files`               | List uploaded files              |
| `GET`  | `/files/{id}`          | Download a single file           |
| `POST` | `/files/download`      | Download multiple files as ZIP   |
| `GET`  | `/files/{id}/metadata` | Retrieve file metadata           |
| `GET`  | `/health`              | Report that the service is alive |

---

# 1. Upload Files

## `POST /files`

Uploads one or more files using `multipart/form-data`.

### Request

```http
POST /files
Content-Type: multipart/form-data
```

The request must contain one or more `files` fields.

### Example

```bash
curl -X POST http://localhost:8080/files \
  -F "files=@photo.jpg" \
  -F "files=@document.pdf"
```

### Response

**Status:** `201 Created`

```json
{
  "files": [
    {
      "id": "01JXYZ123",
      "name": "photo.jpg",
      "size": 245812,
      "content_type": "image/jpeg"
    },
    {
      "id": "01JXYZ124",
      "name": "document.pdf",
      "size": 182341,
      "content_type": "application/pdf"
    }
  ]
}
```

### Response Fields

| Field          | Type      | Description            |
| -------------- | --------- | ---------------------- |
| `id`           | `string`  | Unique file identifier |
| `name`         | `string`  | Original filename      |
| `size`         | `integer` | File size in bytes     |
| `content_type` | `string`  | MIME type              |

### Errors

| Status | Description                   |
| ------ | ----------------------------- |
| `400`  | Invalid multipart request     |
| `413`  | File exceeds the allowed size |
| `500`  | Internal server error         |

---

# 2. List Files

## `GET /files`

Returns a list of uploaded files.

### Request

```http
GET /files
```

Optional pagination parameters:

```http
GET /files?page=1&page_size=20
```

### Query Parameters

| Parameter   | Type      | Default | Description              |
| ----------- | --------- | ------: | ------------------------ |
| `page`      | `integer` |     `1` | Page number              |
| `page_size` | `integer` |    `20` | Number of files per page |

### Response

**Status:** `200 OK`

```json
{
  "files": [
    {
      "id": "01JXYZ123",
      "name": "photo.jpg",
      "size": 245812,
      "content_type": "image/jpeg",
      "created_at": "2026-08-07T06:30:00Z"
    },
    {
      "id": "01JXYZ124",
      "name": "document.pdf",
      "size": 182341,
      "content_type": "application/pdf",
      "created_at": "2026-08-07T06:31:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 2
  }
}
```

---

# 3. Download a File

## `GET /files/{id}`

Downloads the file associated with the specified ID.

### Path Parameters

| Parameter | Type     | Description            |
| --------- | -------- | ---------------------- |
| `id`      | `string` | Unique file identifier |

### Request

```http
GET /files/01JXYZ123
```

### Response

**Status:** `200 OK`

```http
Content-Type: image/jpeg
Content-Disposition: attachment; filename="photo.jpg"
```

The response body contains the raw binary file content.

### Errors

| Status | Description           |
| ------ | --------------------- |
| `404`  | File does not exist   |
| `500`  | Internal server error |

---

# 4. Download Multiple Files

## `POST /files/download`

Downloads multiple files as a ZIP archive.

### Request

```http
POST /files/download
Content-Type: application/json
```

```json
{
  "file_ids": ["01JXYZ123", "01JXYZ124"]
}
```

### Example

```bash
curl -X POST http://localhost:8080/files/download \
  -H "Content-Type: application/json" \
  -d '{
    "file_ids": [
      "01JXYZ123",
      "01JXYZ124"
    ]
  }' \
  --output files.zip
```

### Response

**Status:** `200 OK`

```http
Content-Type: application/zip
Content-Disposition: attachment; filename="files.zip"
```

The response body contains the ZIP archive.

### Errors

| Status | Description                           |
| ------ | ------------------------------------- |
| `400`  | Invalid request or empty file ID list |
| `404`  | One or more files do not exist        |
| `500`  | Internal server error                 |

---

# 5. Get File Metadata

## `GET /files/{id}/metadata`

Returns metadata for a file without downloading its contents.

### Request

```http
GET /files/01JXYZ123/metadata
```

### Response

**Status:** `200 OK`

```json
{
  "id": "01JXYZ123",
  "name": "photo.jpg",
  "size": 245812,
  "content_type": "image/jpeg",
  "created_at": "2026-08-07T06:30:00Z"
}
```

### Response Fields

| Field          | Type      | Description                                |
| -------------- | --------- | ------------------------------------------ |
| `id`           | `string`  | Unique file identifier                     |
| `name`         | `string`  | Original filename                          |
| `size`         | `integer` | File size in bytes                         |
| `content_type` | `string`  | MIME type                                  |
| `created_at`   | `string`  | File creation timestamp in ISO 8601 format |

### Errors

| Status | Description           |
| ------ | --------------------- |
| `404`  | File does not exist   |
| `500`  | Internal server error |

---

# 6. Health Check

## `GET /health`

Reports whether the File Server process is alive.

### Request

```http
GET /health
```

### Response

**Status:** `200 OK`

```json
{
  "status": "ok"
}
```

---

# Error Response

Errors should use a consistent JSON structure.

```json
{
  "error": {
    "code": "FILE_NOT_FOUND",
    "message": "File 01JXYZ123 does not exist"
  }
}
```

## Error Fields

| Field     | Type     | Description                      |
| --------- | -------- | -------------------------------- |
| `code`    | `string` | Machine-readable error code      |
| `message` | `string` | Human-readable error description |

## Common Error Codes

| HTTP Status | Code              | Description                            |
| ----------: | ----------------- | -------------------------------------- |
|       `400` | `INVALID_REQUEST` | Request is malformed or invalid        |
|       `404` | `FILE_NOT_FOUND`  | Requested file does not exist          |
|       `413` | `FILE_TOO_LARGE`  | File exceeds the configured size limit |
|       `500` | `INTERNAL_ERROR`  | Unexpected server error                |

---

# API Responsibilities

The HTTP API is exposed by the **File Server**.

The File Server is responsible for:

- HTTP request handling
- Request validation
- Multipart parsing
- HTTP response formatting
- ZIP generation for multi-file downloads
- Communication with the Storage Service

The Storage Service is responsible for:

- File persistence
- File retrieval
- File metadata
- File deletion/storage operations
- Filesystem access

The File Server must **not directly access the Storage Service's filesystem**.

```text
Frontend
   │
   │ HTTP
   ▼
File Server
   │
   │ gRPC
   ▼
Storage Service
   │
   ▼
Filesystem
```

This separation allows the storage implementation to be replaced later without changing the public HTTP API.
