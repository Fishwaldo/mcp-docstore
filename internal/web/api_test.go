// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"fmt"
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
