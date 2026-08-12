# syntax=docker/dockerfile:1

# ============================
# Build stage
# ============================
FROM golang:1.26.5-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates wget

ENV GOWORK=off

RUN GRPC_HEALTH_PROBE_VERSION=v0.4.24 && \
    wget -qO/bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe

COPY gen/ ./gen/
COPY services/storage/go.mod services/storage/go.sum ./services/storage/

# Download dependencies
WORKDIR /build/services/storage
RUN go mod download

# Copy source code
COPY services/storage/ .

# Build the binary
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s" -o /bin/storage-server ./cmd/server \
    && chmod +x /bin/storage-server

# ============================
# Final stage
# ============================
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary
COPY --from=builder /bin/storage-server /app/storage-server

COPY --from=builder /bin/grpc_health_probe /app/grpc_health_probe

# Copy migrations (needed if you run migrate from entrypoint)
COPY --from=builder /build/services/storage/db/migrations /app/db/migrations

# Copy CA certificates for gRPC TLS (if needed later)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Storage data directory
VOLUME ["/data"]

# gRPC port
EXPOSE 50051

USER nonroot:nonroot

ENTRYPOINT ["/app/storage-server"]