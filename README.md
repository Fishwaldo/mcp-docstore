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

It is its own **OAuth 2.1 authorization server**: it federates login to your existing identity
provider (Okta, or any OIDC IdP), mints its own audience-bound access tokens, and resolves each
caller to a **tenant** by email domain or address. Data is isolated per tenant, with org-wide,
private, and shared projects. Connecting an MCP client is paste-a-URL — no per-client OAuth app
to register on your IdP.

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

Copy [`config.example.yaml`](config.example.yaml) — it is the fully-commented reference — and
edit it. Every key with a listed default may be omitted. The complete surface:

```yaml
listen_addr: ":8080"                       # HTTP listen address (default ":8080")
public_url: "https://docs.example.com"     # public base URL; the OAuth issuer + resource identifier.
                                           # Used in discovery/protected-resource metadata, WWW-Authenticate, and icon URLs
snapshot_retention: 10                     # versions kept per document (default 10)
bleve_index_path: "./data/index.bleve"     # required; full-text index location
session_timeout: 2m                        # reap idle Streamable HTTP MCP sessions (default 2m; must be positive)
max_request_bytes: 4194304                 # cap MCP request body bytes (default 4 MiB)

logging:
  level: info                              # debug | info | warn | error (default info)
  format: json                             # json | text (default json)
  client_ip_header: ""                     # e.g. "X-Forwarded-For" behind a proxy; empty = connection RemoteAddr

database:
  driver: sqlite                           # sqlite | mysql | postgres
  # sqlite REQUIRES _pragma=foreign_keys(1) in the DSN — the server fails fast at startup without it
  dsn: "file:./data/docstore.db?_pragma=foreign_keys(1)"

oidc:                                      # the UPSTREAM identity provider — login-federation leg ONLY.
                                           # This server is its own OAuth issuer (see oauth: below).
  issuer: "https://idp.example.com"
  client_id: "docstore-upstream-client-id"          # the ONE confidential client you register on your IdP
  client_secret: "docstore-upstream-client-secret"  # redirect URI: {public_url}/oauth/callback
  scopes: ["openid", "profile", "email", "groups", "offline_access"]  # default; offline_access is REQUIRED for refresh
  discovery_timeout: 15s                   # bounds OIDC discovery + JWKS refresh calls (default 15s)
  # allow_private_ip: false                # true ONLY for an internal IdP resolving to an RFC-1918/loopback address
                                           # (relaxes SSRF protection on IdP calls; logged as a startup warning)
  # root_ca: "/etc/mcp-docstore/idp-ca.pem"  # extra CA PEM to trust for an internal IdP's TLS certificate

oauth:                                     # the embedded OAuth 2.1 authorization server (always on)
  access_token_ttl: 15m                    # issued access-token lifetime (default 15m)
  refresh_token_ttl: 168h                  # issued refresh-token lifetime (default 168h / 7 days)
  registration: "open"                     # "open" (default; any client may dynamically register) | "allowlist"
  # registration_allowlist:                # required (>=1 https:// entry) when registration is "allowlist"
  #   - "https://client.example.com/callback"
  cookie_secure: true                      # HTTPS-only consent cookie (default true); set false ONLY for local http dev
  sweep_interval: 1h                       # reap expired oauth rows: auth codes, refresh tokens, revocations (default 1h)
  trust_proxy: false                       # trust proxy X-Forwarded-* headers for client IP/scheme (default false)
  trusted_proxy_count: 1                   # proxy hops to peel off when trust_proxy is true (default 1)

web:                                       # optional web UI + REST API; omit or set enabled: false to disable
  enabled: true                            # serves the SPA at "/", bearer-authenticated /api/*, and public /openapi.json + /docs

tenants:
  - key: acme
    name: "Acme Corp"
    match:
      domains: ["acme.com", "acme.io"]     # a caller's email domain
      emails: ["contractor@gmail.com"]     # or exact email
    admins: ["alice@acme.com"]             # tenant admins (declarative; reconciled at login)
```

Tenant admins have full read/write over every project in their own tenant. The `admins` list
is the single source of truth and is reconciled on each login. See the [Authentication](#authentication)
and [Web UI and REST API](#web-ui-and-rest-api) sections below for what the `oidc:`, `oauth:`, and
`web:` blocks actually do.

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

MCP DocStore is its own **OAuth 2.1 authorization server** (built on
[mcp-oauth](https://github.com/giantswarm/mcp-oauth)): it federates login to your upstream IdP
(the `oidc:` block) and mints its own short-lived, audience-bound access tokens, verified by one
shared pipeline for both `/mcp` and (when the web UI is enabled) `/api`. It
serves the full OAuth surface itself — `/oauth/{authorize,callback,token,revoke,register}` plus
`/.well-known/{oauth-authorization-server,openid-configuration,oauth-protected-resource,jwks.json}`
— so clients discover everything from the MCP URL alone; there is no separate Okta app per
client.

#### For users — connect an MCP client

No client registration, no client ID/secret to obtain. Just point your client at the server:

- **claude.ai** (custom connector): paste `{public_url}/mcp` as the connector URL. Dynamic
  client registration (RFC 7591) and PKCE handle the rest.
- **Claude Code**:
  ```sh
  claude mcp add --transport http docstore https://docs.example.com/mcp
  ```
  No `--client-id`, `--client-secret`, or `--callback-port` flags — open dynamic registration
  plus RFC 8252 loopback redirects handle it.

The first time a given client connects, DocStore shows a one-time **consent screen** ("`<client
name>` wants to sign you in through this server's identity provider") before forwarding to your
upstream IdP login. Approval is remembered (cookie-based, ~90 days); the first-party web UI is
exempt and never shows it.

#### For operators — deploy

You need **exactly one** confidential OIDC client registered on your upstream IdP (e.g. one Okta
app), with a single redirect URI:

```
{public_url}/oauth/callback
```

That one upstream client is shared by every downstream MCP client and the web UI — none of them
talk to the IdP directly. No API Access Management / custom-authorization-server product is
required on the IdP side; DocStore issues its own tokens.

Grant the upstream app the **`offline_access`** scope (it is in the default `oidc.scopes`). The
IdP only returns a refresh token when this scope is granted, and DocStore needs that upstream
refresh token to renew its cached provider token as sessions rotate. Without it, refreshes fail
once the cached provider token lapses and clients are forced back through full login.

```yaml
oidc:
  issuer: "https://idp.example.com"
  client_id: "docstore-upstream-client-id"
  client_secret: "docstore-upstream-client-secret"
  # allow_private_ip: true       # only for an internal IdP resolving to an RFC-1918/loopback address
  # root_ca: "/etc/mcp-docstore/idp-ca.pem"   # PEM for an internal CA signing the IdP's certificate

oauth:
  registration: "open"           # "open" (default, any client may register) | "allowlist"
  # registration_allowlist:      # required (>=1 https:// entry) when registration is "allowlist"
  #   - "https://client.example.com/callback"
```

`oauth.registration: allowlist` confines dynamic client registration to an exact-match list of
HTTPS redirect URIs — use it to restrict which third-party clients may ever register, instead of
leaving registration open to anyone who can reach the server.

Every request to `/mcp` (and to `/api/*` when the web UI is enabled) carries
`Authorization: Bearer <token>`; the server verifies its own signature (no network call to the
IdP on this path), `exp`, `iss`, and `aud`, resolves the email to a tenant, and rejects an
invalid/expired/revoked token with `401` plus a `WWW-Authenticate` challenge pointing at the
[RFC 9728](https://datatracker.ietf.org/doc/rfc9728) protected-resource metadata (`/api` instead
returns `403` for a token that's valid but resolves to no tenant — see the Web UI section below).

**Rotating signing keys.** Signing keys are generated automatically on first boot and stored
(alongside a master secret used to derive at-rest encryption and cookie-signing keys) in the
`oauth_keys` database row. To rotate, delete that row and restart the server: it regenerates
fresh key material immediately. This is a disruptive operational event — **every outstanding
access and refresh token is invalidated**, and every MCP client and web UI user must re-login.

> **Breaking change (upgrading from the resource-server version):** DocStore is no longer a pure
> OAuth resource server — it now issues its own tokens. Removed config keys: `oidc.audience`
> (the server fails fast at startup with a migration message if this is still set),
> `oidc.email_claim`, `oidc.groups_claim`, `oidc.email_verified_policy`, `oidc.discovery_url`, and
> the entire per-client web block (`web.client_id`, `web.client_secret`, `web.redirect_url`,
> `web.post_logout_redirect_url`, `web.scopes`). Replace your old two-app Okta setup (one MCP
> resource-server app, one web app) with the single upstream `oidc.client_id`/`client_secret`
> client described above, plus the new `oauth:` block. Existing MCP clients and web UI sessions
> must re-authenticate once against the new flow; there is no way to carry over a resource-server
> bearer token.

### Web UI and REST API

An optional browser-facing UI can be enabled by adding a `web:` block to the config. It serves
the built single-page app at `/`, a read-only REST API at `/api/*`, and public API documentation
at `/openapi.json` + `/docs` (also mirrored at `/api/openapi.json` + `/api/docs`) —
**unauthenticated**, since an API spec isn't a secret:

```yaml
web:
  enabled: true
```

When `web:` is absent (or `enabled: false`) the server is MCP-only: `/`, `/api/*`, and the
spec/docs routes all 404, and `/mcp` plus the OAuth/discovery routes are the only active surface.

**The SPA is a public OAuth client, not a session-backed BFF.** There is no server-side session,
cookie, or CSRF token for the web UI at all. `docstore-web` — seeded automatically on boot as a
**public** client (no secret; PKCE proves possession of the authorization code) and exempted from
the human consent page as first-party — drives its own Authorization Code + PKCE flow directly
against this server's own `/oauth/{authorize,token,revoke}` from inside the browser. `/api/*`
verifies the resulting bearer tokens through the exact same in-process verifier `/mcp` uses
(signature, issuer, expiry, audience, revocation) — there is no BFF process or session store
sitting between the browser and that verifier.

Tokens live only in the browser tab: the access token (15-minute default lifetime,
`oauth.access_token_ttl`) sits in a module-scoped JS variable that's gone on reload or tab close,
and the refresh token lives in `sessionStorage` (gone on tab close, never shared across tabs).
Refresh tokens rotate on every use, and reusing an already-rotated one revokes its whole family —
so a stolen refresh token has a narrow window before the legitimate tab's next refresh detects the
theft and kills it. **This is a deliberate trade-off, not a claim that XSS risk is eliminated**:
a successful script injection in the SPA can still exfiltrate whatever token is live in that tab
at that instant. The mitigations are the short access-token lifetime (bounding the blast radius),
per-tab (not shared-across-tabs) `sessionStorage`, rotation + reuse detection on refresh, a strict
`Content-Security-Policy` (`script-src 'self'`, no inline scripts) on every response the SPA
serves, and server-side sanitization of any user-authored HTML that reaches the page (search
snippets, rendered markdown). **Logout is DocStore-only**: it revokes the refresh token
(`POST /oauth/revoke`) and clears local browser state, but never touches the upstream IdP's own
session — a user who logs out and clicks sign-in again may be silently re-authenticated if their
upstream IdP session is still live. Ending that upstream session is a separate action against the
IdP itself.

#### External REST API clients

Any OAuth client — not just the bundled SPA — can reach `/api/*` through the same recipe:

1. **Register** (`POST /oauth/register`, RFC 7591 dynamic client registration) with your own
   redirect URI.
2. **Authorize**: send the user's browser to `GET /oauth/authorize` with a PKCE challenge. Since
   your client isn't first-party, the user sees DocStore's one-time consent page before being
   forwarded to the upstream IdP.
3. **Exchange** the returned code at `POST /oauth/token` for an access + refresh token pair.
4. **Call** `/api/*` with `Authorization: Bearer <access_token>` — the identical bearer pipeline
   `/mcp` uses.

Two limitations to design around:
- **There is no `client_credentials` (machine-to-machine) grant.** Every token chain traces back
  to a human completing a browser login through the upstream IdP at least once; a purely
  unattended/service client cannot mint its first token on its own.
- **A refresh token idle longer than `oauth.refresh_token_ttl` (default 168h / 7 days) expires.**
  A client that goes dormant past that window must repeat the authorize → consent → exchange
  steps; there is no server-side keep-alive for an unused refresh token.

> **Breaking change (removing the web BFF):** the web UI is no longer a confidential OAuth client
> with a server-side session — it is the public-client SPA described above. Removed config keys:
> `web.client_id`, `web.client_secret`, `web.redirect_url`, `web.post_logout_redirect_url`,
> `web.scopes`, `web.cookie_secure`, `web.idle_timeout`, `web.absolute_timeout`,
> `web.sweep_interval` — `web:` now has exactly one key, `enabled`. `web.cookie_secure` and
> `web.sweep_interval` reappear, renamed and AS-wide rather than web-only, as
> `oauth.cookie_secure` and `oauth.sweep_interval` (there is no idle/absolute session timeout to
> configure any more — the SPA's session lifetime is just its access/refresh token lifetimes).
> Default token lifetimes also changed: `oauth.access_token_ttl` is now **15m** (was 1h) and
> `oauth.refresh_token_ttl` is now **168h** / 7 days (was 720h / 30 days).
>
> The old BFF's session table (`sessions` in the database) is now orphaned: ent's auto-migration
> only creates/alters tables for schemas still declared in `internal/ent/schema`, it never drops
> ones that were removed. Upgrading operators must drop it by hand:
> ```sql
> DROP TABLE sessions;
> ```
> Existing web UI users are simply signed out by the upgrade (there is no migration from a
> server-side session to the in-browser token model) and must sign in again; no MCP client is
> affected.

## Architecture

Layered, with each package owning one job:

| Package | Responsibility |
|---|---|
| `internal/config` | Viper config load + validation |
| `internal/tenant` | email/domain → tenant resolution + admin lookup |
| `internal/auth` | bearer verification of the server's own self-issued JWTs against in-process keys (shared by `/mcp` and `/api`) + email→tenant identity resolution |
| `internal/oauthsrv` | embedded OAuth 2.1 authorization server: signing keys, upstream federation, consent gate, route mounting |
| `internal/ent` | generated [ent](https://entgo.io) data layer |
| `internal/store` | repository: the access rule, tenant scoping, optimistic concurrency, snapshots |
| `internal/docs` | goldmark markdown section editing + diffs |
| `internal/search` | Bleve index: build, query, rebuild |
| `internal/index` | the only bridge between `store` and `search` (keeps the index in sync) |
| `internal/mcp` | MCP tool definitions + thin handlers + elicitation |
| `internal/web` | optional bearer-gated `/api` REST surface + embedded SPA static serving |
| `cmd/server` | boot, HTTP wiring, CLI |

Built on the [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk).

## Development

```sh
go test ./...                                                          # full suite (uses in-memory SQLite)
go test -race ./internal/mcp/... ./cmd/... ./internal/oauthsrv/...     # race detector on the concurrent paths
go generate ./...                                                      # regenerate ent after a schema change
```

## License

[MIT](LICENSE) © 2026 Justin Hammond. Dependencies are permissively licensed (MIT / BSD / Apache-2.0).
