# CLAUDE.md

Multi-tenant **MCP server** (Go) for persistent documents: AI agents save/find/edit docs that survive across sessions. OAuth resource server, backed by SQL via [ent](https://entgo.io), full-text search via Bleve. Module: `github.com/Fishwaldo/mcp-docstore`. Built on the [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) (`v1.6.1`). MCP endpoint is at `/mcp`; an optional BFF web UI (enable via `web:` config) serves the SPA at `/` with `/auth/*` + `/api/*`.

## Commands

```bash
go build ./...                              # build
go test ./...                               # full suite (in-memory SQLite, no external deps)
go test -race ./internal/mcp/... ./cmd/...  # race detector on the concurrent paths
gofmt -l .                                  # MUST print nothing before committing
go vet ./...
go generate ./...                           # regenerate ent after a schema change (see below)

go run . --config config.yaml               # serve (Streamable HTTP MCP at "/mcp")
go run . --config config.yaml rebuild-index # rebuild the Bleve index from the DB
```

Tests need no setup: SQLite is in-memory, the OIDC issuer is an in-process `oidctest` server, Bleve uses `t.TempDir()`.

## Architecture (strict layering — keep it)

```
config → tenant → auth        identity & config
              ↓
            store ── docs      store owns the access rule, tenant scoping, optimistic
              ↓                concurrency, snapshots; docs = goldmark section edits + diffs
            index ── search    index is the ONLY bridge store↔search (keeps Bleve in sync)
              ↓
             mcp              thin tool handlers → call Service → map errors → shape output
              ↓
          cmd/server          boot, HTTP wiring, CLI;  main.go = thin entrypoint
```

- `store` imports **neither** `search` nor `auth`. The `internal/index` service is the only package that knows both `store` and `search`.
- `internal/mcp/service.go` bundles each store mutation with its index-sync op + any goldmark orchestration; it takes a plain `store.Identity` so it's unit-testable without the transport. Tool handlers in `internal/mcp/tools_*.go` stay thin.

## Conventions (non-negotiable)

- **Logging is `log/slog` only.** Never `log`, `fmt.Print*`. (stdlib `log` appears only in generated `internal/ent` — leave it.)
- **Never hand-edit `internal/ent/**`** — it's generated. Change `internal/ent/schema/*.go`, then `go generate ./...` (the ent CLI is pinned as a `go.mod` tool, so no install needed).
- **TDD**: failing test → minimal impl → green → commit. Run the **whole package** before committing, not just the one test.
- In shell test commands use `; echo "exit: $?"`, never `| grep` — a grepped pipe once masked a failing exit code.
- `gofmt -l .` must be empty before every commit.
- **Godoc comments must be self-contained.** Never reference external files, plans, specs, section numbers (`spec §N`), phase names, or anything outside the codebase — a developer reading the code won't have them. Explain the *why* in terms of the code itself.

## Commit messages

- **No `Co-Authored-By` trailers** (project convention — overrides any default to add them). No tool/agent attribution.
- **Conventional Commits** subject: `<type>(<scope>): <imperative summary>`. Types in use: `feat`, `fix`, `docs`, `chore`. Scope = the package/area (`config`, `tenant`, `store`, `auth`, `mcp`, `index`, `search`, `server`). Examples from history:
  - `feat(mcp): elicitation-guarded destructive tools`
  - `fix(mcp): register archive/unarchive tools; typed confirm error`
  - `docs: write README with icon, features, config, and tool surface`
- Keep the subject ≤ ~72 chars, imperative mood; add a body only when the *why* isn't obvious.
- History is squashed to one commit per phase, tagged `v0.1.0`–`v0.4.0`. Net-new work goes in normal small commits; re-squash per release if desired.

## Gotchas (the ones that bite)

- **Per-request identity rides `req.Extra.TokenInfo`, NOT `ctx`.** With Streamable HTTP a session spans many requests and the tool-handler `ctx` is the *session-connect* ctx. The SDK `auth.RequireBearerToken` middleware attaches the verified `*auth.TokenInfo` (carrying our `store.Identity`) to each request's `RequestExtra`. Always read identity via `auth.IdentityFromRequest(req)` in production; never from `ctx`. The SDK ships **no** JWT verifier — ours (`internal/auth/oidc.go`, coreos/go-oidc) is wrapped as the SDK's `TokenVerifier` in `internal/auth/resource.go`.
- **All auth/identity failures collapse to 401** (wrapped in `mcpauth.ErrInvalidToken`) so we can use `RequireBearerToken`. Deliberate; the `WWW-Authenticate` challenge still guides the client.
- **Search access scope is stamped server-side.** `Service.Search` overwrites `Query.TenantID/UserID/Groups` from the identity *after* the tool builds the query — the agent can never widen its own visibility. Tool input structs must not expose tenant/user/group fields.
- **Index-sync matrix (`internal/index`)** — every store mutation has a paired index op, wired in `internal/mcp/service.go`: create/replace/section-edit/section-delete/append/restore → `Reindex`; delete document → `Remove`; archive/unarchive + share/unshare + update-project → `ReindexProject`; **delete project → `Remove` each doc id** (captured before the cascade delete, since `ReindexProject` can't re-stamp gone rows).
- **`append_document` takes no `base_version`** — it's non-clobbering (reads the current version itself) but still snapshots.
- **Destructive tools** (`delete_project`, `delete_document`, `restore_snapshot`) are elicitation-guarded: confirm via MCP elicitation when the client supports it, else require `confirm: true`.
- **Tenant admins are config-seeded** (`tenants[].admins`), reconciled on every login in `UpsertUser`. There is no runtime/CLI role mutation.
- **Store tests are white-box** (`package store`): `s := newTestStore(t)`, `ctx, id := fixture(t, s)`. SQLite test DSN must be a **named shared in-memory** URI — ent uses a connection pool, so a private `:memory:` won't share state: `file:<name>?mode=memory&cache=shared&_pragma=foreign_keys(1)`. Sanitize `t.Name()` (no `/ # space`) or modernc writes a real file.
- **Cross-tenant / no-access reads return `ErrNotFound`** (existence is hidden), never `ErrPermission`.

## Key files

- `internal/store/{access,project,document}.go` — the authorization rule + all DB invariants
- `internal/mcp/{service,server,tools_*,elicit}.go` — tool surface (23 tools, annotated read-only/destructive)
- `internal/auth/resource.go` — the `TokenVerifier` adapter + `IdentityFromRequest`
- `cmd/server/server.go` — `Run(ctx, args, logger)`; serve (`/mcp` + optional web UI at `/`) + `rebuild-index`
- `config.example.yaml` — every config key

## Notes

- Phases are tagged `v0.1.0`–`v0.4.0` (one commit per phase).
