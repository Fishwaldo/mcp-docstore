// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// registerAPI mounts the 10 read-only JSON operations on the Huma API instance.
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
		OperationID:   "create-project",
		Method:        http.MethodPost,
		Path:          "/projects",
		Summary:       "Create a project",
		DefaultStatus: http.StatusCreated,
	}, s.handleCreateProject)

	huma.Register(api, huma.Operation{
		OperationID: "update-project",
		Method:      http.MethodPatch,
		Path:        "/projects/{id}",
		Summary:     "Update a project's name, description, or visibility",
	}, s.handleUpdateProject)

	huma.Register(api, huma.Operation{
		OperationID: "list-documents",
		Method:      http.MethodGet,
		Path:        "/projects/{id}/documents",
		Summary:     "List documents in a project",
	}, s.handleListDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "list-shares",
		Method:      http.MethodGet,
		Path:        "/projects/{id}/shares",
		Summary:     "List a project's user and group shares",
	}, s.handleListShares)

	huma.Register(api, huma.Operation{
		OperationID: "add-shares",
		Method:      http.MethodPost,
		Path:        "/projects/{id}/shares",
		Summary:     "Add or update user/group shares on a project",
	}, s.handleAddShares)

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

	huma.Register(api, huma.Operation{
		OperationID: "get-tags",
		Method:      http.MethodGet,
		Path:        "/tags",
		Summary:     "List the distinct tags across the caller's accessible documents",
	}, s.handleGetTags)

	huma.Register(api, huma.Operation{
		OperationID: "edit-document",
		Method:      http.MethodPatch,
		Path:        "/documents/{id}",
		Summary:     "Edit a document (full replace of provided fields, optimistic concurrency)",
	}, s.handleEditDocument)

	huma.Register(api, huma.Operation{
		OperationID:   "create-document",
		Method:        http.MethodPost,
		Path:          "/documents",
		Summary:       "Create a document in a project",
		DefaultStatus: http.StatusCreated,
	}, s.handleCreateDocument)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-document",
		Method:        http.MethodDelete,
		Path:          "/documents/{id}",
		Summary:       "Delete a document",
		DefaultStatus: http.StatusNoContent,
	}, s.handleDeleteDocument)

	huma.Register(api, huma.Operation{
		OperationID: "restore-snapshot",
		Method:      http.MethodPost,
		Path:        "/documents/{id}/restore",
		Summary:     "Restore a document to a snapshot version (body-only by default)",
	}, s.handleRestoreSnapshot)

	huma.Register(api, huma.Operation{
		OperationID: "archive-project",
		Method:      http.MethodPost,
		Path:        "/projects/{id}/archive",
		Summary:     "Archive a project (hide from listings; reversible)",
	}, s.handleArchiveProject)

	huma.Register(api, huma.Operation{
		OperationID: "unarchive-project",
		Method:      http.MethodPost,
		Path:        "/projects/{id}/unarchive",
		Summary:     "Unarchive a project",
	}, s.handleUnarchiveProject)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-project",
		Method:        http.MethodDelete,
		Path:          "/projects/{id}",
		Summary:       "Delete a project and all its documents",
		DefaultStatus: http.StatusNoContent,
	}, s.handleDeleteProject)

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/me",
		Summary:     "Get the caller's resolved identity",
	}, s.handleMe)
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
		dtos[i] = toProjectDTO(p.Project, p.Access.String(), p.CanManage)
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
	pa, err := s.svc.GetProjectWithAccess(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &getProjectOutput{Body: toProjectDTO(pa.Project, pa.Access.String(), pa.CanManage)}, nil
}

// --- create-project ---

type createProjectInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1"`
		Description string `json:"description,omitempty"`
		Visibility  string `json:"visibility"`
	}
}

type createProjectOutput struct {
	Body ProjectDTO
}

func (s *Server) handleCreateProject(ctx context.Context, in *createProjectInput) (*createProjectOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	p, err := s.svc.CreateProject(ctx, id, in.Body.Name, in.Body.Description, in.Body.Visibility)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	// Re-fetch to build a DTO with the caller's access + can_manage consistently (the
	// created entity may not carry the Owner edge). The creator is the owner.
	pa, err := s.svc.GetProjectWithAccess(ctx, id, p.ID)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &createProjectOutput{Body: toProjectDTO(pa.Project, pa.Access.String(), pa.CanManage)}, nil
}

// --- update-project ---

type updateProjectInput struct {
	ID   string `path:"id"`
	Body struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
		Visibility  *string `json:"visibility,omitempty"`
	}
}

type updateProjectOutput struct {
	Body ProjectDTO
}

func (s *Server) handleUpdateProject(ctx context.Context, in *updateProjectInput) (*updateProjectOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.svc.UpdateProject(ctx, id, pid, store.ProjectUpdate{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Visibility:  in.Body.Visibility,
	}); err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	pa, err := s.svc.GetProjectWithAccess(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &updateProjectOutput{Body: toProjectDTO(pa.Project, pa.Access.String(), pa.CanManage)}, nil
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

// --- list-shares ---

type listSharesInput struct {
	ID string `path:"id"`
}

type listSharesOutput struct {
	Body ShareDTO
}

func (s *Server) handleListShares(ctx context.Context, in *listSharesInput) (*listSharesOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	shares, err := s.svc.ListShares(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &listSharesOutput{Body: toShareDTO(shares)}, nil
}

// --- add-shares ---

type shareMutationInput struct {
	ID   string `path:"id"`
	Body struct {
		Kind       string   `json:"kind" doc:"\"user\" or \"group\""`
		Principals []string `json:"principals" doc:"emails (kind=user) or group names (kind=group)"`
		Permission string   `json:"permission" doc:"\"read\" or \"write\""`
	}
}

type addSharesOutput struct {
	Body struct {
		Shares     ShareDTO `json:"shares"`
		Unresolved []string `json:"unresolved"`
	}
}

func (s *Server) handleAddShares(ctx context.Context, in *shareMutationInput) (*addSharesOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	var res *store.ShareResult
	switch in.Body.Kind {
	case "user":
		res, err = s.svc.ShareUsers(ctx, id, pid, in.Body.Principals, in.Body.Permission)
	case "group":
		res, err = s.svc.ShareGroups(ctx, id, pid, in.Body.Principals, in.Body.Permission)
	default:
		return nil, huma.Error400BadRequest(`kind must be "user" or "group"`)
	}
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	shares, err := s.svc.ListShares(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	out := &addSharesOutput{}
	out.Body.Shares = toShareDTO(shares)
	out.Body.Unresolved = res.Unresolved
	if out.Body.Unresolved == nil {
		out.Body.Unresolved = []string{}
	}
	return out, nil
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

// --- me ---

type meInput struct{}

type meBody struct {
	Email  string   `json:"email"`
	Tenant string   `json:"tenant"`
	Groups []string `json:"groups"`
}

type meOutput struct {
	Body meBody
}

// handleMe reports the caller's own resolved identity: the email and tenant key recorded on
// their user/tenant rows (the durable, provisioned values — not whatever the token's claims
// said this request), plus the groups the token carried, which RequireBearer stamped onto the
// identity as-is.
func (s *Server) handleMe(ctx context.Context, _ *meInput) (*meOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	u, err := s.store.EntClient().User.Get(ctx, id.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("load user: " + err.Error())
	}
	ten, err := s.store.EntClient().Tenant.Get(ctx, id.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("load tenant: " + err.Error())
	}
	return &meOutput{Body: meBody{Email: u.Email, Tenant: ten.Key, Groups: id.Groups}}, nil
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

// --- get-tags ---

type getTagsInput struct{}

type getTagsOutput struct {
	Body struct {
		Tags []string `json:"tags"`
	}
}

func (s *Server) handleGetTags(ctx context.Context, _ *getTagsInput) (*getTagsOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	tags, err := s.svc.ListTags(ctx, id)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	out := &getTagsOutput{}
	out.Body.Tags = tags
	return out, nil
}

// --- edit-document ---

type editDocumentInput struct {
	ID   string `path:"id"`
	Body struct {
		BaseVersion int       `json:"base_version" minimum:"1" doc:"Document version the edit is based on; stale values are rejected with 409"`
		Overview    *string   `json:"overview,omitempty"`
		Body        *string   `json:"body,omitempty"`
		Tags        *[]string `json:"tags,omitempty"`
		Comment     string    `json:"comment,omitempty"`
	}
}

type editDocumentOutput struct {
	Body DocumentDTO
}

func (s *Server) handleEditDocument(ctx context.Context, in *editDocumentInput) (*editDocumentOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	docID, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	doc, err := s.svc.EditReplace(ctx, id, docID, in.Body.BaseVersion, in.Body.Overview, in.Body.Body, in.Body.Tags, in.Body.Comment)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	html, err := renderMarkdown(doc.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	return &editDocumentOutput{Body: toDocumentDTO(doc, html)}, nil
}

// --- create-document ---

type createDocumentInput struct {
	Body struct {
		ProjectID string   `json:"project_id" doc:"Project the document belongs to"`
		Title     string   `json:"title" minLength:"1"`
		Overview  string   `json:"overview,omitempty"`
		BodyMD    string   `json:"body,omitempty"`
		Tags      []string `json:"tags,omitempty"`
		Comment   string   `json:"comment,omitempty"`
	}
}

type createDocumentOutput struct {
	Body DocumentDTO
}

func (s *Server) handleCreateDocument(ctx context.Context, in *createDocumentInput) (*createDocumentOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.Body.ProjectID)
	if err != nil {
		return nil, err
	}
	doc, err := s.svc.CreateDocument(ctx, id, pid, store.NewDocument{
		Title:    in.Body.Title,
		Overview: in.Body.Overview,
		Body:     in.Body.BodyMD,
		Tags:     in.Body.Tags,
		Comment:  in.Body.Comment,
	})
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	html, err := renderMarkdown(doc.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	return &createDocumentOutput{Body: toDocumentDTO(doc, html)}, nil
}

// --- delete-document ---

type deleteDocumentInput struct {
	ID string `path:"id"`
}

type deleteDocumentOutput struct{}

func (s *Server) handleDeleteDocument(ctx context.Context, in *deleteDocumentInput) (*deleteDocumentOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	docID, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	if err := s.svc.DeleteDocument(ctx, id, docID); err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &deleteDocumentOutput{}, nil
}

// --- archive/unarchive-project ---

type projectActionInput struct {
	ID string `path:"id"`
}

type projectActionOutput struct {
	Body ProjectDTO
}

// projectDTOAfter re-fetches a project's DTO after a mutation that doesn't itself return
// the updated entity (archive/unarchive/delete all just return error). An archived project
// stays reachable by id via GetProjectWithAccess, so this works for the archive path too.
func (s *Server) projectDTOAfter(ctx context.Context, id store.Identity, pid uuid.UUID) (*projectActionOutput, error) {
	pa, err := s.svc.GetProjectWithAccess(ctx, id, pid)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &projectActionOutput{Body: toProjectDTO(pa.Project, pa.Access.String(), pa.CanManage)}, nil
}

func (s *Server) handleArchiveProject(ctx context.Context, in *projectActionInput) (*projectActionOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	if err := s.svc.ArchiveProject(ctx, id, pid); err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return s.projectDTOAfter(ctx, id, pid)
}

func (s *Server) handleUnarchiveProject(ctx context.Context, in *projectActionInput) (*projectActionOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	if err := s.svc.UnarchiveProject(ctx, id, pid); err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return s.projectDTOAfter(ctx, id, pid)
}

// --- delete-project ---

type deleteProjectOutput struct{}

func (s *Server) handleDeleteProject(ctx context.Context, in *projectActionInput) (*deleteProjectOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	pid, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	if err := s.svc.DeleteProject(ctx, id, pid); err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	return &deleteProjectOutput{}, nil
}

// --- restore-snapshot ---

type restoreSnapshotInput struct {
	ID   string `path:"id"`
	Body struct {
		Version     int    `json:"version" minimum:"1" doc:"Snapshot version to restore"`
		BaseVersion int    `json:"base_version" minimum:"1" doc:"Current document version the caller has read"`
		Scope       string `json:"scope,omitempty" enum:"body,full" doc:"body (default) restores only the body; full also restores overview and tags"`
		Comment     string `json:"comment,omitempty"`
	}
}

type restoreSnapshotOutput struct {
	Body DocumentDTO
}

func (s *Server) handleRestoreSnapshot(ctx context.Context, in *restoreSnapshotInput) (*restoreSnapshotOutput, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return nil, huma.Error500InternalServerError("missing identity")
	}
	docID, err := parseUUID(in.ID)
	if err != nil {
		return nil, err
	}
	doc, err := s.svc.RestoreSnapshot(ctx, id, docID, in.Body.Version, in.Body.BaseVersion, store.RestoreScope(in.Body.Scope), in.Body.Comment)
	if err != nil {
		return nil, huma.NewError(httpStatusForError(err), err.Error())
	}
	html, err := renderMarkdown(doc.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("render failed: " + err.Error())
	}
	return &restoreSnapshotOutput{Body: toDocumentDTO(doc, html)}, nil
}
