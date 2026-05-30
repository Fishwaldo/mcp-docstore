# syntax=docker/dockerfile:1

# Build on the native build platform and cross-compile to the target arch
# (CGO is off — modernc.org/sqlite is pure Go — so no QEMU is needed to build).
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/mcp-docstore .

# Pre-create the data dir so it can be copied in with nonroot ownership below.
RUN mkdir -p /data

# Minimal runtime: distroless static ships CA certs (needed for OIDC discovery
# over HTTPS) and runs as a non-root user (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mcp-docstore /usr/local/bin/mcp-docstore

# Persistent state lives here: the SQLite database (when DB_DRIVER=sqlite) and the
# Bleve index. Owned by the nonroot uid so the server can write to it. Point
# bleve_index_path and the sqlite dsn under /data, and mount a volume at /data.
COPY --from=build --chown=65532:65532 /data /data
VOLUME ["/data"]

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mcp-docstore"]
# Run with a config + persistent data volume, e.g.:
#   docker run -p 8080:8080 \
#     -v mcp-docstore-data:/data \
#     -v $PWD/config.yaml:/etc/mcp-docstore/config.yaml:ro \
#     ghcr.io/fishwaldo/mcp-docstore --config /etc/mcp-docstore/config.yaml
