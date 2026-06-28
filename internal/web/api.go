// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/search"
)

// registerAPI mounts the 9 read-only JSON operations on the Huma API instance.
// Every handler reads identity from the request context via IdentityFromContext;
// the session middleware stamps it there before any handler runs. A missing identity
// is treated as a server error (middleware normally guarantees it is present).
func (s *Server) registerAPI(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/projects",
		Summary:     "List projects the caller can access",
	}, s.handleListProjects)

	huma.Register(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/projects/{id}",
		Summary:     "Get a project by ID",
	}, s.handleGetProject)

	huma.Register(api, huma.Operation{
		OperationID: "list-documents",
		Method:      http.MethodGet,
		Path:        "/projects/{id}/documents",
		Summary:     "List documents in a project",
	}, s.handleListDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "get-document",
		Method:      http.MethodGet,
		Path:        "/documents/{id}",
		Summary:     "Get a document with rendered HTML body",
	}, s.handleGetDocument)

	huma.Register(api, huma.Operation{
		OperationID: "get-section",
		Method:      http.MethodGet,
		Path:        "/documents/{id}/section",
		Summary:     "Get a single section from a document",
	}, s.handleGetSection)

	huma.Register(api, huma.Operation{
		OperationID: "list-snapshots",
		Method:      http.MethodGet,
		Path:        "/documents/{id}/snapshots",
		Summary:     "List version snapshots for a document",
	}, s.handleListSnapshots)

	huma.Register(api, huma.Operation{
		OperationID: "get-snapshot",
		Method:      http.MethodGet,
		Path:        "/documents/{id}/snapshots/{version}",
		Summary:     "Get a specific version snapshot",
	}, s.handleGetSnapshot)

	huma.Register(api, huma.Operation{
		OperationID: "diff-versions",
		Method:      http.MethodGet,
		Path:        "/documents/{id}/diff",
		Summary:     "Get a unified diff between two document versions",
	}, s.handleDiff)

	huma.Register(api, huma.Operation{
		OperationID: "search",
		Method:      http.MethodGet,
		Path:        "/search",
		Summary:     "Full-text search across accessible documents",
	}, s.handleSearch)
}

// parseUUID parses a path-parameter UUID string and returns a 400 error on failure.
func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, huma.Error400BadRequest("invalid UUID: " + s)
	}
	return id, nil
}

// --- list-projects ---

type listProjectsInput struct {
	IncludeArchived bool `query:"include_archived"`
}

type listProjectsOutput struct {
	Body []ProjectDTO
}

func (s *Server) handleListProjects(ctx context.Context, in *listProjectsInput) (*listProjectsOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	projects, err := s.svc.ListProjectsWithAccess(ctx, id, in.IncludeArchived)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	dtos := make([]ProjectDTO, len(projects))
	for i, p := range projects {
		dtos[i] = toProjectDTO(p.Project, p.Access.String())
	}
	return &listProjectsOutput{Body: dtos}, nil
}

// --- get-project ---

type getProjectInput struct {
	ID string `path:"id"`
}

type getProjectOutput struct {
	Body ProjectDTO
}

func (s *Server) handleGetProject(ctx context.Context, in *getProjectInput) (*getProjectOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	p, err := s.svc.GetProject(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &getProjectOutput{Body: toProjectDTO(p, "")}, nil
}

// --- list-documents ---

type listDocumentsInput struct {
	ID string `path:"id"`
}

type listDocumentsOutput struct {
	Body []DocumentSummaryDTO
}

func (s *Server) handleListDocuments(ctx context.Context, in *listDocumentsInput) (*listDocumentsOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	docs, err := s.svc.ListDocuments(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	dtos := make([]DocumentSummaryDTO, len(docs))
	for i, d := range docs {
		dtos[i] = toDocumentSummary(d)
	}
	return &listDocumentsOutput{Body: dtos}, nil
}

// --- get-document ---

type getDocumentInput struct {
	ID string `path:"id"`
}

type getDocumentOutput struct {
	Body DocumentDTO
}

func (s *Server) handleGetDocument(ctx context.Context, in *getDocumentInput) (*getDocumentOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	did, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	doc, err := s.svc.GetDocument(ctx, id, did)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	html, err := renderMarkdown(doc.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	return &getDocumentOutput{Body: toDocumentDTO(doc, html)}, nil
}

// --- get-section ---

type getSectionInput struct {
	ID      string `path:"id"`
	Heading string `query:"heading"`
}

type getSectionBody struct {
	Heading string `json:"heading"`
	HTML    string `json:"html"`
}

type getSectionOutput struct {
	Body getSectionBody
}

func (s *Server) handleGetSection(ctx context.Context, in *getSectionInput) (*getSectionOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	did, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	md, err := s.svc.GetSection(ctx, id, did, in.Heading)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	html, err := renderMarkdown(md)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	return &getSectionOutput{Body: getSectionBody{Heading: in.Heading, HTML: html}}, nil
}

// --- list-snapshots ---

type listSnapshotsInput struct {
	ID string `path:"id"`
}

type listSnapshotsOutput struct {
	Body []SnapshotDTO
}

func (s *Server) handleListSnapshots(ctx context.Context, in *listSnapshotsInput) (*listSnapshotsOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	did, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	snaps, err := s.svc.ListSnapshots(ctx, id, did)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	dtos := make([]SnapshotDTO, len(snaps))
	for i, sn := range snaps {
		dtos[i] = toSnapshotDTO(sn)
	}
	return &listSnapshotsOutput{Body: dtos}, nil
}

// --- get-snapshot ---

type getSnapshotInput struct {
	ID      string `path:"id"`
	Version int    `path:"version"`
}

type getSnapshotOutput struct {
	Body SnapshotDTO
}

func (s *Server) handleGetSnapshot(ctx context.Context, in *getSnapshotInput) (*getSnapshotOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	did, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	snap, err := s.svc.GetSnapshot(ctx, id, did, in.Version)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	dto := toSnapshotDTO(snap)
	html, err := renderMarkdown(snap.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	dto.BodyHTML = html
	return &getSnapshotOutput{Body: dto}, nil
}

// --- diff ---

type diffInput struct {
	ID   string `path:"id"`
	From int    `query:"from"`
	To   int    `query:"to"`
}

type diffBody struct {
	Diff string `json:"diff"`
}

type diffOutput struct {
	Body diffBody
}

func (s *Server) handleDiff(ctx context.Context, in *diffInput) (*diffOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	did, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	diff, err := s.svc.DiffVersions(ctx, id, did, in.From, in.To)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &diffOutput{Body: diffBody{Diff: diff}}, nil
}

// --- search ---

type searchInput struct {
	Q          string   `query:"q"`
	ProjectID  string   `query:"project_id"`
	Visibility string   `query:"visibility"`
	Tags       []string `query:"tags"`
	Limit      int      `query:"limit"`
}

type searchOutput struct {
	Body []SearchHitDTO
}

func (s *Server) handleSearch(ctx context.Context, in *searchInput) (*searchOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	q := search.Query{
		Text:       in.Q,
		Visibility: in.Visibility,
		Tags:       in.Tags,
		Limit:      in.Limit,
	}
	if in.ProjectID != "" {
		pid, err := parseUUID(in.ProjectID)
		if err != nil {
			return nil, err
		}
		q.ProjectID = pid.String()
	}
	// Access scope (TenantID/UserID/Groups) is stamped server-side by Service.Search.
	results, err := s.svc.Search(ctx, id, q)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	dtos := make([]SearchHitDTO, len(results))
	for i, r := range results {
		dtos[i] = toSearchHit(r)
	}
	return &searchOutput{Body: dtos}, nil
}
