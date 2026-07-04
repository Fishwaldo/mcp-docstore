# CLAUDE.md

Multi-tenant **MCP server** (Go) for persistent documents: AI agents save/find/edit docs that survive across sessions. It is its own **OAuth 2.1 authorization server** (federates login upstream to Okta/any OIDC IdP, mints its own tokens), backed by SQL via [ent](https://entgo.io), full-text search via Bleve. Module: `github.com/Fishwaldo/mcp-docstore`. Built on the [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) (`v1.6.1`) and [mcp-oauth](https://github.com/giantswarm/mcp-oauth) for the AS internals. MCP endpoint is at `/mcp`; the AS serves `/oauth/*` + `/.well-known/*`; an optional web UI (enable via `web.enabled` config) serves the embedded SPA at `/` and a bearer-gated REST API at `/api/*` — no BFF, no server-side session, no cookies. `/api` and `/mcp` share one bearer-verification pipeline: the SPA is a public OAuth client (PKCE, no secret) that holds its own tokens in the browser.

## Commands

```bash
go build ./...                                                      # build
go test ./...                                                       # full suite (in-memory SQLite, no external deps)
go test -race ./internal/mcp/... ./cmd/... ./internal/oauthsrv/...  # race detector on the concurrent paths
gofmt -l .                                                          # MUST print nothing before committing
go vet ./...
go generate ./...                                                   # regenerate ent after a schema change (see below)

go run . --config config.yaml               # serve (Streamable HTTP MCP at "/mcp", AS at "/oauth/*" + "/.well-known/*")
go run . --config config.yaml rebuild-index # rebuild the Bleve index from the DB
```

Tests need no setup: SQLite is in-memory, the upstream OIDC issuer is an in-process `idptest` server, Bleve uses `t.TempDir()`.

## Architecture (strict layering — keep it)

```
config → tenant → auth        identity & config
              ↓
        oauthsrv (keys, upstream federation, consent, mount)   embedded OAuth 2.1 AS;
              ↓                                                 auth.LocalVerifier checks its tokens in-process
            store ── docs      store owns the access rule, tenant scoping, optimistic
              ↓                concurrency, snapshots; docs = goldmark section edits + diffs
            index ── search    index is the ONLY bridge store↔search (keeps Bleve in sync)
              ↓
          mcp ── web           mcp: thin tool handlers → Service → map errors → shape output;
              ↓                web (optional): bearer-gated /api + SPA static serving, same verifier
          cmd/server          boot, HTTP wiring, CLI;  main.go = thin entrypoint
```

- `store` imports **neither** `search` nor `auth`. The `internal/index` service is the only package that knows both `store` and `search`.
- `internal/mcp/service.go` bundles each store mutation with its index-sync op + any goldmark orchestration; it takes a plain `store.Identity` so it's unit-testable without the transport. Tool handlers in `internal/mcp/tools_*.go` stay thin.
- `internal/oauthsrv` is the embedded OAuth 2.1 authorization server: `keys.go` loads/creates the persistent signing-key + master-secret row and derives every other secret (at-rest encryption, consent-cookie HMAC key) from it via HKDF; `oauthsrv.go` assembles the upstream (dex-compatible) provider + `mcp-oauth` `server.Server`/`handler.Handler` and seeds the first-party `docstore-web` client as a **public** client (no secret, PKCE-only); `consent.go` gates `/oauth/authorize` behind a one-time approval page for any client that isn't `docstore-web`; `mount.go` wires it all onto the shared `http.ServeMux`. `internal/oauthsrv/entstore` adapts `mcp-oauth`'s storage interfaces onto the same ent client the rest of the app uses. `internal/auth` carries two verifiers: `oidc.go`'s `OIDCVerifier` (JWKS-fetching, used only during upstream federation) and `local.go`'s `LocalVerifier` (static in-process key set, what actually guards both `/mcp` and `/api`). `internal/auth/identity_pipeline.go`'s `VerifyRequestIdentity` is the single verify→resolve sequence every transport shares. `internal/web` is the optional REST API + SPA static server: it has no session, cookie, or CSRF state of its own — `bearer.go`'s `RequireBearer` calls the same `VerifyRequestIdentity` pipeline `/mcp` uses, over a real HTTP hop from the browser (not an in-process transport).

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

- **Per-request identity rides `req.Extra.TokenInfo`, NOT `ctx`, on the `/mcp` transport.** With Streamable HTTP a session spans many requests and the tool-handler `ctx` is the *session-connect* ctx. The SDK `auth.RequireBearerToken` middleware attaches the verified `*auth.TokenInfo` (carrying our `store.Identity`) to each request's `RequestExtra`. Always read identity via `auth.IdentityFromRequest(req)` in production; never from `ctx`. `/api` has no such wrinkle — `internal/web.RequireBearer` stamps identity straight onto the request `ctx` (`IdentityFromContext`), since it's a plain `http.Handler` chain, not a long-lived session. The SDK ships **no** JWT verifier — both `/mcp` and `/api` are guarded by the *same* `internal/auth/local.go` `LocalVerifier` instance, which checks our own AS-issued tokens against a static in-process public key set (`oauthsrv.Service.PublicKeys()`) — no JWKS HTTP fetch, no dependency on our own public URL being reachable. It's constructed once in `cmd/server.Run` with `audiences = [{public_url}/mcp, {public_url}]`, so a token minted for either audience clears both routes. `internal/auth/oidc.go`'s `OIDCVerifier` still exists but is only used for the upstream federation leg inside `oauthsrv`. Both are wrapped as the SDK's `TokenVerifier` in `internal/auth/resource.go`; `internal/auth/identity_pipeline.go`'s `VerifyRequestIdentity` is the shared verify→resolve call both `resource.go` and `internal/web/bearer.go` build on.
- **`/mcp` collapses every auth/identity failure to 401** (wrapped in `mcpauth.ErrInvalidToken`) so it can use `RequireBearerToken`; the `WWW-Authenticate` challenge still guides the client. **`/api` does not collapse the distinction**: `internal/web.RequireBearer` returns 401 `{"error":"invalid_token"}` for a bad/expired/revoked token, but 403 `{"error":"no_access"}` for a token that verifies fine but resolves to no tenant (`errors.Is(err, auth.ErrIdentityRejected)`) — the caller proved who they are but isn't provisioned. Same underlying `VerifyRequestIdentity` call, two different HTTP mappings per transport.
- **The `oidc:` config block is the UPSTREAM IdP, not the resource-server verifier it used to be.** DocStore is its own OAuth issuer now (the `oauth:` block); `oidc:` only configures the "log in via my company IdP" federation hop inside `internal/oauthsrv`. `oidc.audience` is a removed key: `config.Validate` detects it non-empty and fails fast with a migration message (checked *before* the `client_id`/`client_secret` required-field checks, so a genuine stale pre-AS config gets the actionable error instead of a generic one).
- **`/oauth/*` and `/.well-known/*` are always mounted**, independent of `web.enabled`. `oauthsrv.Service.Mount` registers the library's routes (`/oauth/{authorize,callback,token,revoke,register}`, `/.well-known/{oauth-authorization-server,openid-configuration,jwks.json,oauth-protected-resource}`) on an inner mux, with `consentGate` intercepting `GET /oauth/authorize` first — everything else passes straight through. `internal/web.Mount`'s two root aliases, `GET /openapi.json` and `GET /docs`, are only mounted when `web.enabled` is true, but are **never** behind `RequireBearer` — they, plus their `/api/openapi.json`/`/api/docs` originals, are reachable with no `Authorization` header at all (an API spec isn't a secret).
- **`docstore-web` is a PUBLIC AS client, not a BFF with its own credentials.** There is no session, cookie, or CSRF state anywhere in `internal/web` any more. `cmd/server.Run` calls `asvc.SeedWebClient(ctx)` to idempotently register `docstore-web` as `storage.ClientTypePublic` with `token_endpoint_auth_method: none` (no secret at all — PKCE is what proves possession of the code); the SPA (`web/src/lib/oauth.ts`) then drives its own Authorization Code + PKCE flow directly from the browser against `/oauth/{authorize,token,revoke}`, and `internal/web.RequireBearer` just verifies the resulting bearer token like any other caller — there is no in-process transport hop from `web` to `oauthsrv` to reason about.
- **New third-party clients hit a consent gate before their first upstream login.** `internal/oauthsrv/consent.go`'s `consentGate` forwards `GET /oauth/authorize` straight through only for the first-party `docstore-web` (`webClientID`) or a client already covered by a valid (HMAC-verified) consent cookie; anything else sees the approval page first. This is what prevents a maliciously dynamically-registered client from silently redirecting a user's browser to the upstream IdP.
- **`oidc.allow_private_ip` is an SSRF opt-out**, needed only when the upstream IdP resolves to an RFC-1918/loopback address (an internal Dex/Keycloak). It relaxes the discovery/token/JWKS HTTP calls in `oauthsrv.New`'s `dex.NewProvider` construction and logs a WARN at startup; `oidc.root_ca` trusts an additional PEM CA for the same calls.
- **Key rotation = delete the `oauth_keys` row + restart.** `oauthsrv.LoadOrCreateKeyMaterial` generates a fresh ES256 signing key + master secret only when that singleton row is absent; deleting it forces regeneration on the next boot, which invalidates every outstanding access/refresh token (all derived secrets change too) and forces every client to re-login. There is no in-place rotation path.
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
- `internal/auth/resource.go` — the `TokenVerifier` adapter + `IdentityFromRequest` (the `/mcp` transport)
- `internal/auth/identity_pipeline.go` — `VerifyRequestIdentity`/`ErrIdentityRejected`: the verify→resolve sequence `/mcp` and `/api` both build on
- `internal/auth/local.go` — `LocalVerifier`: validates our own AS-issued tokens against a static in-process key set (no JWKS fetch), audience-checked against `[{public_url}/mcp, {public_url}]`
- `internal/oauthsrv/{oauthsrv,keys,consent,mount}.go` — the embedded AS: server assembly + public `docstore-web` client seeding, key material load/create + HKDF derivation, confused-deputy consent gate, route mounting
- `internal/oauthsrv/entstore/` — adapts `mcp-oauth`'s storage interfaces onto the shared ent client
- `internal/web/bearer.go` — `RequireBearer`: the `/api` transport's bearer check (401 `invalid_token` vs 403 `no_access`)
- `internal/web/mount.go` — `/api/*` route wiring + the unauthenticated `/openapi.json`, `/docs` root aliases
- `web/src/lib/oauth.ts` — the SPA's own PKCE flow (access token in memory, refresh token in `sessionStorage`)
- `cmd/server/server.go` — `Run(ctx, args, logger)`; serve (`/mcp` + `/oauth/*` + `/.well-known/*` + optional web UI at `/` and `/api/*`) + `rebuild-index`
- `config.example.yaml` — every config key

## Notes

- Phases are tagged `v0.1.0`–`v0.4.0` (one commit per phase); later work is tagged `v0.4.1`–`v0.5.0` and beyond.
