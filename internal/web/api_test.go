// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// newAPIServer creates a Server wired with a real in-memory store and an app.Service,
// seeds a tenant + user, and returns the server, the store, and the seeded identity.
func newAPIServer(t *testing.T) (*Server, *store.Store, store.Identity) {
	t.Helper()
	srv, st, _ := newTestServer(t)

	ctx := context.Background()
	ten, err := st.EnsureTenant(ctx, "acme", "Acme") // idempotent: newTestServer seeds it
	require.NoError(t, err)
	u, err := st.UpsertUser(ctx, ten.ID, "api-s1", "alice@acme.com", false)
	require.NoError(t, err)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID, Groups: []string{"eng"}}

	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })

	svc := app.NewService(st, index.New(st, idx), nil)
	srv.svc = svc
	return srv, st, id
}

// withIdentity wraps a context with the identity the API handlers expect.
func withIdentity(ctx context.Context, id store.Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey, id)
}

// doGet issues a GET to a fresh test API with the identity stamped on the context.
func doGet(t *testing.T, srv *Server, id store.Identity, path string) *httptest.ResponseRecorder {
	t.Helper()
	_, api := humatest.New(t)
	srv.registerAPI(api)
	return api.GetCtx(withIdentity(context.Background(), id), path)
}

// decodeJSON unmarshals the JSON response body into dst.
// Huma serialises the Body field directly — no {"body":...} envelope.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(dst))
}

// --- list-projects ---

func TestListProjectsHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	_, err := st.CreateProject(ctx, id, "Alpha", "desc", "org")
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/projects")
	require.Equal(t, 200, rec.Code)

	var projects []ProjectDTO
	decodeJSON(t, rec, &projects)
	require.NotEmpty(t, projects)
	found := false
	for _, p := range projects {
		if p.Name == "Alpha" {
			found = true
		}
	}
	require.True(t, found, "expected Alpha project in list")
}

func TestListProjectsNoIdentityReturns500(t *testing.T) {
	srv, _, _ := newAPIServer(t)
	_, api := humatest.New(t)
	srv.registerAPI(api)
	rec := api.Get("/projects")
	require.Equal(t, 500, rec.Code)
}

// --- get-document (worked tests from brief) ---

func TestGetDocumentHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{
		Title: "Hello", Body: "# Heading\n\nSome text.",
	})
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/documents/"+d.ID.String())
	require.Equal(t, 200, rec.Code)

	var doc DocumentDTO
	decodeJSON(t, rec, &doc)
	require.Equal(t, "Hello", doc.Title)
	require.Contains(t, doc.BodyHTML, "<h1")
}

func TestGetDocumentIncludesRawBody(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id) // body "# Doc One\n\nhello body\n"

	rec := doGet(t, srv, id, "/documents/"+docID)
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var dto DocumentDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, "# Doc One\n\nhello body\n", dto.Body)
	require.Contains(t, dto.BodyHTML, "<h1") // rendered HTML still present
}

func TestGetDocumentCrossTenantReturns404(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()

	// Seed a second tenant with its own user+project+doc.
	ten2, err := st.EnsureTenant(ctx, "other", "Other Corp")
	require.NoError(t, err)
	u2, err := st.UpsertUser(ctx, ten2.ID, "sub-2", "bob@other.com", false)
	require.NoError(t, err)
	id2 := store.Identity{TenantID: ten2.ID, UserID: u2.ID}
	p2, err := st.CreateProject(ctx, id2, "P2", "", "private")
	require.NoError(t, err)
	d2, err := srv.svc.CreateDocument(ctx, id2, p2.ID, store.NewDocument{Title: "Secret"})
	require.NoError(t, err)

	// Caller from tenant "acme" must not read tenant "other"'s doc.
	rec := doGet(t, srv, id, "/documents/"+d2.ID.String())
	require.Equal(t, 404, rec.Code)
}

func TestGetDocumentInvalidUUIDReturns400(t *testing.T) {
	srv, _, id := newAPIServer(t)
	rec := doGet(t, srv, id, "/documents/not-a-uuid")
	require.Equal(t, 400, rec.Code)
}

// --- get-project ---

func TestGetProjectHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "Beta", "", "org")
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/projects/"+p.ID.String())
	require.Equal(t, 200, rec.Code)

	var proj ProjectDTO
	decodeJSON(t, rec, &proj)
	require.Equal(t, "Beta", proj.Name)
}

func TestGetProjectNotFoundReturns404(t *testing.T) {
	srv, _, id := newAPIServer(t)
	rec := doGet(t, srv, id, "/projects/00000000-0000-0000-0000-000000000000")
	require.Equal(t, 404, rec.Code)
}

func TestGetProjectIncludesAccessLevel(t *testing.T) {
	srv, _, id := newAPIServer(t)
	projectID, _ := seedProjectAndDoc(t, srv, id) // org project, caller is owner → write

	rec := doGet(t, srv, id, "/projects/"+projectID)
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var dto ProjectDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, "write", dto.Access)
}

func TestGetProjectCanManage(t *testing.T) {
	srv, st, id := newAPIServer(t)
	projectID, _ := seedProjectAndDoc(t, srv, id) // org project owned by the caller

	// Owner: can_manage true.
	rec := doGet(t, srv, id, "/projects/"+projectID)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	var owner ProjectDTO
	decodeJSON(t, rec, &owner)
	require.True(t, owner.CanManage)
	require.Equal(t, "write", owner.Access)

	// A different, non-admin member of the same tenant: org access grants write,
	// but NOT can_manage.
	ctx := context.Background()
	ten, err := st.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u2, err := st.UpsertUser(ctx, ten.ID, "api-s2", "bob@acme.com", false)
	require.NoError(t, err)
	id2 := store.Identity{TenantID: ten.ID, UserID: u2.ID, Groups: []string{"eng"}}

	rec2 := doGet(t, srv, id2, "/projects/"+projectID)
	require.Equal(t, 200, rec2.Code, rec2.Body.String())
	var member ProjectDTO
	decodeJSON(t, rec2, &member)
	require.Equal(t, "write", member.Access) // org membership
	require.False(t, member.CanManage)       // but not owner/admin
}

func TestCreateProject(t *testing.T) {
	srv, _, id := newAPIServer(t)

	body := map[string]any{"name": "New Project", "description": "d", "visibility": "private"}
	rec := doJSON(t, srv, id, "POST", "/projects", body)
	require.Equal(t, 201, rec.Code, rec.Body.String())

	var dto ProjectDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, "New Project", dto.Name)
	require.Equal(t, "private", dto.Visibility)
	require.Equal(t, "write", dto.Access)
	require.True(t, dto.CanManage) // creator is owner
	require.NotEmpty(t, dto.ID)

	// Invalid visibility → 400.
	bad := map[string]any{"name": "X", "visibility": "public"}
	recBad := doJSON(t, srv, id, "POST", "/projects", bad)
	require.Equal(t, 400, recBad.Code, recBad.Body.String())
}

// --- list-documents ---

func TestListDocumentsHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "Docs", "", "private")
	require.NoError(t, err)
	_, err = srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "D1"})
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/projects/"+p.ID.String()+"/documents")
	require.Equal(t, 200, rec.Code)

	var docs []DocumentSummaryDTO
	decodeJSON(t, rec, &docs)
	require.Len(t, docs, 1)
	require.Equal(t, "D1", docs[0].Title)
}

// --- get-section ---

func TestGetSectionHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "Sec", "", "private")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{
		Title: "S", Body: "# Alpha\n\nalpha content\n\n# Beta\n\nbeta content",
	})
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/documents/"+d.ID.String()+"/section?heading=Alpha")
	require.Equal(t, 200, rec.Code)

	var sec getSectionBody
	decodeJSON(t, rec, &sec)
	require.Equal(t, "Alpha", sec.Heading)
	require.Contains(t, sec.HTML, "alpha content")
}

// --- list-snapshots ---

func TestListSnapshotsHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "Snap", "", "private")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "S"})
	require.NoError(t, err)
	body2 := "updated"
	_, err = srv.svc.EditReplace(ctx, id, d.ID, d.Version, nil, &body2, nil, "v2")
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/documents/"+d.ID.String()+"/snapshots")
	require.Equal(t, 200, rec.Code)

	var snaps []SnapshotDTO
	decodeJSON(t, rec, &snaps)
	require.NotEmpty(t, snaps)
}

// --- get-snapshot ---

func TestGetSnapshotHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "GS", "", "private")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{
		Title: "T", Body: "# Old\n\noriginal content",
	})
	require.NoError(t, err)
	v1 := d.Version
	body2 := "v2 body"
	_, err = srv.svc.EditReplace(ctx, id, d.ID, v1, nil, &body2, nil, "bump")
	require.NoError(t, err)

	rec := doGet(t, srv, id, fmt.Sprintf("/documents/%s/snapshots/%d", d.ID.String(), v1))
	require.Equal(t, 200, rec.Code)

	var snap SnapshotDTO
	decodeJSON(t, rec, &snap)
	require.Equal(t, v1, snap.Version)
	require.Contains(t, snap.BodyHTML, "<h1", "get-snapshot must render the snapshotted body as HTML")
}

// --- diff ---

func TestDiffHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "D", "", "private")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "D", Body: "line1"})
	require.NoError(t, err)
	v1 := d.Version
	body2 := "line1\nline2"
	d2, err := srv.svc.EditReplace(ctx, id, d.ID, v1, nil, &body2, nil, "add line2")
	require.NoError(t, err)

	rec := doGet(t, srv, id, fmt.Sprintf("/documents/%s/diff?from=%d&to=%d", d.ID.String(), v1, d2.Version))
	require.Equal(t, 200, rec.Code)

	var result diffBody
	decodeJSON(t, rec, &result)
	require.Contains(t, result.Diff, "line2")
}

// --- search ---

func TestSearchHappy(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, id, "Srch", "", "org")
	require.NoError(t, err)
	doc, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{
		Title: "Findme", Body: "uniquetoken12345",
	})
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/search?q=uniquetoken12345")
	require.Equal(t, 200, rec.Code)

	var hits []SearchHitDTO
	decodeJSON(t, rec, &hits)
	// Index writes are synchronous in app.Service; the seeded document must appear.
	require.Len(t, hits, 1, "seeded document must be found by its unique term")
	require.Equal(t, doc.ID.String(), hits[0].DocumentID)
}

func TestSearchCrossTenantIsolation(t *testing.T) {
	srv, st, id := newAPIServer(t)
	ctx := context.Background()

	// Second tenant with its own org doc.
	ten2, err := st.EnsureTenant(ctx, "other2", "Other2")
	require.NoError(t, err)
	u2, err := st.UpsertUser(ctx, ten2.ID, "sub-x", "x@other2.com", false)
	require.NoError(t, err)
	id2 := store.Identity{TenantID: ten2.ID, UserID: u2.ID}
	p2, err := st.CreateProject(ctx, id2, "Hidden", "", "org")
	require.NoError(t, err)
	_, err = srv.svc.CreateDocument(ctx, id2, p2.ID, store.NewDocument{
		Title: "Hidden", Body: "secretcrosstoken99",
	})
	require.NoError(t, err)

	rec := doGet(t, srv, id, "/search?q=secretcrosstoken99")
	require.Equal(t, 200, rec.Code)

	var hits []SearchHitDTO
	decodeJSON(t, rec, &hits)
	require.Empty(t, hits, "cross-tenant docs must not appear in search results")
}

// --- get-tags ---

func TestGetTagsReturnsSortedUnion(t *testing.T) {
	srv, _, id := newAPIServer(t)
	seedProjectAndDoc(t, srv, id) // tags ["seed","status:draft"]

	rec := doGet(t, srv, id, "/tags")
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var out struct {
		Tags []string `json:"tags"`
	}
	decodeJSON(t, rec, &out)
	require.Equal(t, []string{"seed", "status:draft"}, out.Tags)
}

// --- me ---

func TestMeHappy(t *testing.T) {
	srv, _, id := newAPIServer(t)

	rec := doGet(t, srv, id, "/me")
	require.Equal(t, 200, rec.Code)

	var me meBody
	decodeJSON(t, rec, &me)
	require.Equal(t, "alice@acme.com", me.Email)
	require.Equal(t, "acme", me.Tenant)
	require.Equal(t, []string{"eng"}, me.Groups)
}

func TestMeNoIdentityReturns500(t *testing.T) {
	srv, _, _ := newAPIServer(t)
	_, api := humatest.New(t)
	srv.registerAPI(api)
	rec := api.Get("/me")
	require.Equal(t, 500, rec.Code)
}

// doJSON issues a write request (PATCH/POST/DELETE) to a fresh test API with the
// identity stamped on the context. body is JSON-encoded by humatest; pass nil for none.
func doJSON(t *testing.T, srv *Server, id store.Identity, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	_, api := humatest.New(t)
	srv.registerAPI(api)
	ctx := withIdentity(context.Background(), id)
	switch method {
	case http.MethodPatch:
		return api.PatchCtx(ctx, path, body)
	case http.MethodPost:
		return api.PostCtx(ctx, path, body)
	case http.MethodDelete:
		return api.DeleteCtx(ctx, path)
	default:
		t.Fatalf("doJSON: unsupported method %s", method)
		return nil
	}
}

// seedProjectAndDoc creates an org project and one document, returning their IDs.
func seedProjectAndDoc(t *testing.T, srv *Server, id store.Identity) (string, string) {
	t.Helper()
	ctx := context.Background()
	p, err := srv.svc.CreateProject(ctx, id, "Write API", "seed", "org")
	require.NoError(t, err)
	d, err := srv.svc.CreateDocument(ctx, id, p.ID, store.NewDocument{
		Title:    "Doc One",
		Overview: "seed overview",
		Body:     "# Doc One\n\nhello body\n",
		Tags:     []string{"seed", "status:draft"},
		Comment:  "seed",
	})
	require.NoError(t, err)
	return p.ID.String(), d.ID.String()
}

func TestEditDocumentReplacesFields(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	newBody := "# Doc One\n\nedited body\n"
	newTags := []string{"seed", "status:done"}
	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1,
		"body":         newBody,
		"tags":         newTags,
		"comment":      "edit via api",
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var dto DocumentDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, 2, dto.Version)
	require.Equal(t, newTags, dto.Tags)
	require.Contains(t, dto.BodyHTML, "edited body")
	require.Equal(t, "seed overview", dto.Overview) // omitted field left unchanged
}

func TestEditDocumentStaleBaseVersionConflicts(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	// First edit bumps the doc to version 2.
	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1, "body": "# Doc One\n\nfirst edit\n",
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	// Second edit still claims base_version 1 → 409 with the current version named.
	rec = doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1, "body": "# Doc One\n\nlost update\n",
	})
	require.Equal(t, 409, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "current version is 2")
}

func TestEditDocumentUnknownIDNotFound(t *testing.T) {
	srv, _, id := newAPIServer(t)
	seedProjectAndDoc(t, srv, id)

	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/00000000-0000-0000-0000-000000000001", map[string]any{
		"base_version": 1, "body": "x",
	})
	require.Equal(t, 404, rec.Code, rec.Body.String())
}

func TestCreateDocument(t *testing.T) {
	srv, _, id := newAPIServer(t)
	projectID, _ := seedProjectAndDoc(t, srv, id)

	rec := doJSON(t, srv, id, http.MethodPost, "/documents", map[string]any{
		"project_id": projectID,
		"title":      "Created via API",
		"overview":   "short abstract",
		"body":       "# Created\n\ncontent\n",
		"tags":       []string{"api"},
		"comment":    "create via api",
	})
	require.Equal(t, 201, rec.Code, rec.Body.String())

	var dto DocumentDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, "Created via API", dto.Title)
	require.Equal(t, 1, dto.Version)
	require.Contains(t, dto.BodyHTML, "content")
	require.NotEmpty(t, dto.ID)
}

func TestCreateDocumentUnknownProjectNotFound(t *testing.T) {
	srv, _, id := newAPIServer(t)

	rec := doJSON(t, srv, id, http.MethodPost, "/documents", map[string]any{
		"project_id": "00000000-0000-0000-0000-000000000001",
		"title":      "Orphan",
	})
	require.Equal(t, 404, rec.Code, rec.Body.String())
}

func TestDeleteDocument(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	rec := doJSON(t, srv, id, http.MethodDelete, "/documents/"+docID, nil)
	require.Equal(t, 204, rec.Code, rec.Body.String())

	// The document is gone: reading it now 404s.
	rec2 := doGet(t, srv, id, "/documents/"+docID)
	require.Equal(t, 404, rec2.Code, rec2.Body.String())
}

func TestDeleteDocumentUnknownIDNotFound(t *testing.T) {
	srv, _, id := newAPIServer(t)
	seedProjectAndDoc(t, srv, id)

	rec := doJSON(t, srv, id, http.MethodDelete, "/documents/00000000-0000-0000-0000-000000000001", nil)
	require.Equal(t, 404, rec.Code, rec.Body.String())
}

// --- restore-snapshot ---

func TestRestoreSnapshotBodyOnlyPreservesLiveMetadata(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	// Bump to v2 with a new body AND new tags. Restoring v1 body-only (the default)
	// must bring back the v1 body while KEEPING the live v2 tags.
	liveTags := []string{"seed", "status:done"}
	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1, "body": "# Doc One\n\nsecond body\n", "tags": liveTags,
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	rec = doJSON(t, srv, id, http.MethodPost, "/documents/"+docID+"/restore", map[string]any{
		"version":      1,
		"base_version": 2,
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var dto DocumentDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, 3, dto.Version)
	require.Contains(t, dto.BodyHTML, "hello body") // v1 body restored
	require.Equal(t, liveTags, dto.Tags)            // live tags preserved
}

func TestRestoreSnapshotFullRestoresMetadata(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1, "body": "# Doc One\n\nsecond body\n", "tags": []string{"changed"},
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	rec = doJSON(t, srv, id, http.MethodPost, "/documents/"+docID+"/restore", map[string]any{
		"version":      1,
		"base_version": 2,
		"scope":        "full",
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	var dto DocumentDTO
	decodeJSON(t, rec, &dto)
	require.Equal(t, []string{"seed", "status:draft"}, dto.Tags) // v1 tags restored
}

func TestRestoreSnapshotStaleBaseVersionConflicts(t *testing.T) {
	srv, _, id := newAPIServer(t)
	_, docID := seedProjectAndDoc(t, srv, id)

	// Bump to v2 first so a v1 snapshot actually exists (snapshots are only
	// created as a side effect of an edit); the restore below targets that
	// snapshot but claims a stale base_version against the now-current v2 doc.
	rec := doJSON(t, srv, id, http.MethodPatch, "/documents/"+docID, map[string]any{
		"base_version": 1, "body": "# Doc One\n\nsecond body\n",
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())

	rec = doJSON(t, srv, id, http.MethodPost, "/documents/"+docID+"/restore", map[string]any{
		"version":      1,
		"base_version": 99,
	})
	require.Equal(t, 409, rec.Code, rec.Body.String())
}
