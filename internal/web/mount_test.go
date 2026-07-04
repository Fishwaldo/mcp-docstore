// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// newMountServer builds a Server wired with a real store and app.Service, seeds a tenant +
// user matching mount-s1/alice@acme.com, and returns the server, the store, the seeded
// identity, and the ES256 key its verifier trusts (for minting bearer tokens with mintToken).
func newMountServer(t *testing.T) (*Server, *store.Store, store.Identity, *ecdsa.PrivateKey) {
	t.Helper()
	srv, st, key := newTestServer(t)

	ctx := context.Background()
	ten, err := st.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u, err := st.UpsertUser(ctx, ten.ID, "mount-s1", "alice@acme.com", false)
	require.NoError(t, err)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID, Groups: []string{"eng"}}

	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })

	svc := app.NewService(st, index.New(st, idx), nil)
	srv.svc = svc
	return srv, st, id, key
}

// bearerToken mints a valid access token for mount-s1/alice@acme.com/["eng"] over key.
func bearerToken(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	return mintToken(t, key, defaultTestClaims("mount-s1", "alice@acme.com", []string{"eng"}))
}

func TestRequireBearerNoHeaderReturns401(t *testing.T) {
	srv, _, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_token", body["error"])
}

func TestRequireBearerGarbageTokenReturns401(t *testing.T) {
	srv, _, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_token", body["error"])
}

// TestRequireBearerValidButTenantlessReturns403 mints a token whose email domain the resolver
// has no tenant mapping for — a valid, correctly-signed token that still cannot be resolved to
// an identity, which RequireBearer must report as 403 no_access, distinct from an invalid token.
func TestRequireBearerValidButTenantlessReturns403(t *testing.T) {
	srv, _, _, key := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	tok := mintToken(t, key, defaultTestClaims("stranger-1", "nobody@unknown.com", nil))

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "no_access", body["error"])
	require.Equal(t, "authenticated but not authorized for any tenant", body["error_description"])
}

// TestRequireBearerStoreErrorReturns500 verifies that an infrastructure fault while resolving
// identity (here: the store's DB connection is already closed) is reported as 500, not the
// terminal 403 no_access a genuine tenantless-user rejection gets — the caller proved who they
// are and would be provisioned if the lookup had succeeded, so the SPA should retry rather than
// render a dead-end "no access" screen.
func TestRequireBearerStoreErrorReturns500(t *testing.T) {
	srv, st, _, key := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	tok := mintToken(t, key, defaultTestClaims("mount-s1", "alice@acme.com", []string{"eng"}))
	require.NoError(t, st.Close())

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())

	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.NotEqual(t, "no_access", body["error"], "an infra fault must not be reported as the terminal no_access body")
}

// TestRequireBearerValidTokenReachesHandler verifies that a valid bearer token both authorizes
// the request and stamps the resolved identity onto the context the handler reads.
func TestRequireBearerValidTokenReachesHandler(t *testing.T) {
	srv, st, id, key := newMountServer(t)
	p, err := st.CreateProject(context.Background(), id, "MountAlpha", "", "org")
	require.NoError(t, err)

	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, key))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), p.ID.String())
}

// TestMountAPIMeReturnsResolvedIdentity drives GET /api/me through the full Mount stack (real
// RequireBearer, not the humatest shortcut api_test.go uses) to confirm the identity RequireBearer
// attaches is exactly what handleMe reads back out.
func TestMountAPIMeReturnsResolvedIdentity(t *testing.T) {
	srv, _, _, key := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, key))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var me meBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&me))
	require.Equal(t, "alice@acme.com", me.Email)
	require.Equal(t, "acme", me.Tenant)
	require.Equal(t, []string{"eng"}, me.Groups)
}

// TestMountPublicDocsAndSpecPaths verifies the four documented public GET paths — the /api
// originals and the root aliases — all serve 200 with no Authorization header, and that each
// root alias serves byte-identical output to its /api original.
func TestMountPublicDocsAndSpecPaths(t *testing.T) {
	srv, _, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	apiSpec := get("/api/openapi.json")
	require.Equal(t, http.StatusOK, apiSpec.Code)

	apiDocs := get("/api/docs")
	require.Equal(t, http.StatusOK, apiDocs.Code)

	rootSpec := get("/openapi.json")
	require.Equal(t, http.StatusOK, rootSpec.Code)
	require.Equal(t, apiSpec.Body.String(), rootSpec.Body.String(), "root alias must serve the same bytes as /api/openapi.json")

	rootDocs := get("/docs")
	require.Equal(t, http.StatusOK, rootDocs.Code)
	require.Equal(t, apiDocs.Body.String(), rootDocs.Body.String(), "root alias must serve the same bytes as /api/docs")
}

// TestMountNonExemptAPIPathsRequireBearer verifies the public exemption is exactly the two
// documented GET paths: neither a POST to the spec path nor any other unauthenticated /api
// request is let through.
func TestMountNonExemptAPIPathsRequireBearer(t *testing.T) {
	srv, _, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "a POST to the spec path must still require a bearer token")

	req2 := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
}
