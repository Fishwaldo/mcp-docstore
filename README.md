<div align="center">
  <img src="assets/icon-512.png" alt="MCP DocStore" width="160" height="160">

  # MCP DocStore

  **A multi-tenant [Model Context Protocol](https://modelcontextprotocol.io) server for persistent documents — so AI agents can save, find, and edit work that survives across sessions and tools.**

  [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
</div>

---

## What it is

MCP DocStore gives AI agents a shared, durable place to keep documents. An agent in one
session (say, a Claude.ai artifact) writes a document; an agent in another (say, Claude Code)
retrieves and edits it later. Documents live in **projects**, each document carries a short
**overview** for quick scanning plus a longer markdown **body**, and every edit is versioned
so nothing is lost.

It is an OAuth **resource server**: it validates bearer tokens from your existing identity
provider and resolves each caller to a **tenant** by email domain or address. Data is isolated
per tenant, with org-wide, private, and shared projects.

## Features

- **Projects & documents** — `org` (whole-tenant) or `private` projects; documents with
  overview + markdown body + tags.
- **Sharing** — share a project with individual users (by email) or with groups drawn from
  the token's `groups` claim; read or write.
- **Versioned edits** — full-replace, section (markdown heading) edits, append, and section
  delete. Mutating edits use optimistic concurrency (`base_version`); every edit snapshots the
  prior version. Inspect history with `list_snapshots` / `get_snapshot` / `diff_versions` and
  roll back with `restore_snapshot`.
- **Full-text search** — keyword search (powered by [Bleve](https://github.com/blevesearch/bleve))
  scoped to exactly what the caller may see; no query syntax to learn.
- **Archiving** — archive/unarchive projects to hide them from lists and search while keeping
  them reachable by id.
- **Safe destructive ops** — `delete_project`, `delete_document`, and `restore_snapshot` are
  confirmation-guarded via MCP **elicitation** (with a `confirm: true` fallback for clients
  that can't prompt).
- **Multi-tenant & config-seeded** — tenants and their admins are declared in config; a user
  belongs to exactly one tenant.

## Tool surface

| Group | Tools |
|---|---|
| Projects | `list_projects`, `create_project`, `get_project`, `update_project`, `archive_project`, `unarchive_project`, `delete_project` |
| Sharing | `share_project`, `unshare_project`, `list_project_shares` |
| Documents | `list_documents`, `create_document`, `get_document`, `get_section`, `edit_document`, `append_document`, `delete_section`, `delete_document` |
| History | `list_snapshots`, `get_snapshot`, `diff_versions`, `restore_snapshot` |
| Search | `search_documents` |

Each tool is annotated with read-only / destructive / closed-world hints so clients can reason
about safety.

## Configuration

Copy [`config.example.yaml`](config.example.yaml) and edit it:

```yaml
listen_addr: ":8080"
public_url: "https://docs.example.com"   # public base URL; used in protected-resource metadata, WWW-Authenticate, and icon URLs
snapshot_retention: 10
bleve_index_path: "./data/index.bleve"

database:
  driver: sqlite                          # sqlite | mysql | postgres
  dsn: "file:./data/docstore.db?_pragma=foreign_keys(1)"

oidc:
  issuer: "https://idp.example.com"
  audience: "mcp-docstore"                # the "aud" this resource server requires
  email_claim: "email"
  groups_claim: "groups"

tenants:
  - key: acme
    name: "Acme Corp"
    match:
      domains: ["acme.com", "acme.io"]
      emails: ["contractor@gmail.com"]
    admins: ["alice@acme.com"]            # tenant admins (declarative; reconciled at login)
```

Tenant admins have full read/write over every project in their own tenant. The `admins` list
is the single source of truth and is reconciled on each login.

## Running

```sh
# build
go build -o mcp-docstore .

# serve (Streamable HTTP MCP endpoint at "/mcp", metadata at /.well-known/oauth-protected-resource)
./mcp-docstore --config config.yaml

# rebuild the search index from the database (after a schema change or index loss)
./mcp-docstore --config config.yaml rebuild-index
```

> **Breaking change:** the MCP endpoint moved from `/` to `/mcp` in v0.5.0. Existing MCP clients must repoint their server URL to `<public_url>/mcp`.

On first boot with an empty index, the server builds it from the database automatically.

### Docker

Images are published to GHCR for `linux/amd64` and `linux/arm64`: `ghcr.io/fishwaldo/mcp-docstore` (tags `:X.Y.Z`, `:X.Y`, `:latest`).

Persistent state — the SQLite database and the Bleve index — lives in the container at **`/data`** (a declared volume, owned by the non-root runtime user `65532`). Point your config there and mount a volume so data survives restarts:

```yaml
# config.yaml (paths under the mounted /data volume)
bleve_index_path: "/data/index.bleve"
database:
  driver: sqlite
  dsn: "file:/data/docstore.db?_pragma=foreign_keys(1)"
```

```sh
docker run -p 8080:8080 \
  -v mcp-docstore-data:/data \
  -v "$PWD/config.yaml:/etc/mcp-docstore/config.yaml:ro" \
  ghcr.io/fishwaldo/mcp-docstore --config /etc/mcp-docstore/config.yaml

# rebuild the index in the same container/volume
docker run --rm \
  -v mcp-docstore-data:/data \
  -v "$PWD/config.yaml:/etc/mcp-docstore/config.yaml:ro" \
  ghcr.io/fishwaldo/mcp-docstore --config /etc/mcp-docstore/config.yaml rebuild-index
```

Notes:
- The container runs as **non-root (uid 65532)**. A Docker **named volume** (as above) is initialized with the right ownership automatically. If you use a **bind mount** instead (`-v $PWD/data:/data`), `chown 65532:65532` the host directory first, or the server can't write to it.
- With an external **MySQL/Postgres** backend, `/data` holds only the Bleve index (rebuildable via `rebuild-index`), not your documents.

Build it yourself:

```sh
docker build -t mcp-docstore .
```

### Authentication

Every request to the MCP endpoint (`/mcp`) must carry `Authorization: Bearer <JWT>`. The server
verifies the token against the configured OIDC issuer (signature, `exp`, `iss`, and `aud`),
resolves the email to a tenant, and rejects unknown identities with `401` plus a
`WWW-Authenticate` challenge pointing at the
[RFC 9728](https://datatracker.ietf.org/doc/rfc9728) protected-resource metadata.

### Web UI (optional, BFF)

A browser-facing session layer can be enabled by adding a `web:` block to the config. It serves
the single-page app at `/`, the OIDC login/callback/logout flow at `/auth/*`, and the read-only
REST API at `/api/*`. The web UI requires a **separate confidential OIDC client** (e.g. an Okta
app) configured to return `groups` and `email` claims; it must not share the MCP bearer-token
client.

```yaml
web:
  client_id: "0oaXXX..."
  client_secret: "secret"
  redirect_url: "https://docs.example.com/auth/callback"
  post_logout_redirect_url: "https://docs.example.com/"
  scopes: ["openid", "email", "profile", "groups"]
  cookie_secure: true           # set false only for local HTTP dev
  idle_timeout: 24h
  absolute_timeout: 168h
  sweep_interval: 1h
```

When `web:` is absent the server is MCP-only: `/mcp` is the sole active endpoint and `/`
returns 404.

## Architecture

Layered, with each package owning one job:

| Package | Responsibility |
|---|---|
| `internal/config` | Viper config load + validation |
| `internal/tenant` | email/domain → tenant resolution + admin lookup |
| `internal/auth` | OIDC token verification, identity resolution (SDK `TokenVerifier`) |
| `internal/ent` | generated [ent](https://entgo.io) data layer |
| `internal/store` | repository: the access rule, tenant scoping, optimistic concurrency, snapshots |
| `internal/docs` | goldmark markdown section editing + diffs |
| `internal/search` | Bleve index: build, query, rebuild |
| `internal/index` | the only bridge between `store` and `search` (keeps the index in sync) |
| `internal/mcp` | MCP tool definitions + thin handlers + elicitation |
| `cmd/server` | boot, HTTP wiring, CLI |

Built on the [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk).

## Development

```sh
go test ./...                              # full suite (uses in-memory SQLite)
go test -race ./internal/mcp/... ./cmd/... # race detector on the concurrent paths
go generate ./...                          # regenerate ent after a schema change
```

## License

[MIT](LICENSE) © 2026 Justin Hammond. Dependencies are permissively licensed (MIT / BSD / Apache-2.0).
