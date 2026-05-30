package mcp

import (
	"errors"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// identityFunc extracts the caller identity from a request (auth.IdentityFromRequest in
// production; a fixed function in tests).
type identityFunc func(*sdk.CallToolRequest) (store.Identity, bool)

type registrar struct {
	svc         *Service
	identityFor identityFunc
	log         *slog.Logger
}

func (r *registrar) ident(req *sdk.CallToolRequest) (store.Identity, error) {
	id, ok := r.identityFor(req)
	if !ok {
		return store.Identity{}, errors.New("unauthenticated")
	}
	return id, nil
}

// serverInstructions is surfaced to clients during initialization (InitializeResult.
// Instructions) as a hint the client can add to the model's system prompt — the
// server-level analog of a tool description. It orients an agent to the doc-store model.
const serverInstructions = `mcp-docstore stores documents so they persist across sessions and agents.

Model:
- Documents live in projects. A project is "org" (visible to everyone in the tenant) or
  "private" (owner plus explicit user/group shares). Use list_projects (it reports your
  access level) and create_project.
- Each document has a short overview (scan this first) and a longer markdown body. Prefer
  list_documents + get_document's overview before fetching or searching full bodies, and
  get_section to pull a single heading's content instead of the whole body.

Finding things:
- Use search_documents with plain keywords (no query syntax); results are scoped to what
  you can access. Archived projects are hidden from lists and search but remain reachable
  by id via get_project/get_document.

Editing safely:
- Mutating edits are optimistic: pass base_version (the version you last read); a stale
  value is rejected with the current version so you can re-read and retry. edit_document
  has mode=replace (overview/body/tags) or mode=section (replace one heading's content);
  delete_section removes a section. append_document adds to the end and never needs a
  base_version (it never clobbers concurrent work). Every edit snapshots the prior version,
  so use list_snapshots, get_snapshot, diff_versions, and restore_snapshot to review or roll back.
- delete_project, delete_document, and restore_snapshot are guarded: confirm when prompted,
  or pass confirm=true if your client cannot prompt.`

// NewMCPServer builds the MCP server with all tools registered.
func NewMCPServer(svc *Service, identityFor identityFunc, log *slog.Logger, icons []sdk.Icon) *sdk.Server {
	if log == nil {
		log = slog.Default()
	}
	srv := sdk.NewServer(
		&sdk.Implementation{Name: "mcp-docstore", Title: "MCP DocStore", Version: "0.4.0", Icons: icons},
		&sdk.ServerOptions{Instructions: serverInstructions, Logger: log},
	)
	r := &registrar{svc: svc, identityFor: identityFor, log: log}
	r.registerProjectTools(srv)
	r.registerDocumentTools(srv)
	r.registerDocumentMoreTools(srv)
	r.registerSnapshotTools(srv)
	r.registerSearchTool(srv)
	r.registerSharingTools(srv)
	r.registerDestructiveTools(srv)
	return srv
}
