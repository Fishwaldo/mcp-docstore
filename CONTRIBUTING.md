# Contributing to mcp-docstore

Thanks for your interest. This is a Go project — please read the short conventions below before opening a PR.

## Getting started

```sh
git clone https://github.com/Fishwaldo/mcp-docstore.git
cd mcp-docstore
go test ./...   # must be green before you touch anything
```

No external services are needed: SQLite runs in-memory, the OIDC server is in-process, and Bleve uses a temp dir.

## Conventions (from CLAUDE.md — these are enforced in CI)

- **`gofmt -l .` must print nothing** before every commit.
- **`go vet ./...` must be clean.**
- **`go test ./...` must pass**, including `go test -race ./internal/mcp/... ./cmd/...`.
- **Logging is `log/slog` only** — never `log` or `fmt.Print*`.
- **Never hand-edit `internal/ent/`** — change `internal/ent/schema/*.go`, then `go generate ./...`.
- **Godoc comments must be self-contained** — don't reference external files, spec sections, or phase names.
- **TDD**: write a failing test first, then implement.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/) style, subject ≤ 72 chars:

```
feat(store): add bulk document import
fix(mcp): register archive/unarchive tools
docs: expand Docker section in README
chore: bump go-sdk to v1.7.0
```

Types in use: `feat`, `fix`, `docs`, `chore`, `ci`, `refactor`, `test`.  
Scope = the package/area: `config`, `tenant`, `store`, `auth`, `mcp`, `index`, `search`, `server`.  
No `Co-Authored-By` trailers.

## Architecture — keep the layers clean

```
config → tenant → auth   (identity)
store ── docs            (data + markdown)
index ── search          (sync bridge + Bleve)
mcp                      (thin tool handlers)
cmd/server               (boot + HTTP)
```

`store` must not import `search` or `auth`. `internal/index` is the only bridge between store and search. Tool handlers in `internal/mcp/tools_*.go` stay thin — validate input, call `Service`, map errors, return output.

## Submitting a PR

1. Fork the repo and create a branch off `main`.
2. Make your changes following the conventions above.
3. Run the full test suite: `go test ./...` and `go test -race ./internal/mcp/... ./cmd/...`.
4. Open a PR — fill in the template.

For large changes (new features, architecture changes) please open an issue first to discuss the approach.

## Reporting bugs

Use the **Bug report** issue template. Include Go version, OS, and steps to reproduce.
