// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// newMountServer builds a Server wired with a real store and app.Service, seeds a
// tenant + user, and returns the server, the store, and the seeded identity.
func newMountServer(t *testing.T) (*Server, *store.Store, store.Identity) {
	t.Helper()
	claims := &auth.Claims{Subject: "mount-s1", Email: "alice@acme.com", Groups: []string{"eng"}}
	srv, st := newTestServer(t, claims)

	ctx := context.Background()
	ten, err := st.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u, err := st.UpsertUser(ctx, ten.ID, claims.Subject, claims.Email, false)
	require.NoError(t, err)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID, Groups: claims.Groups}

	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })

	svc := app.NewService(st, index.New(st, idx), nil)
	srv.svc = svc
	return srv, st, id
}

// seedSession creates a live session for id in st and returns the raw token.
func seedSession(t *testing.T, st *store.Store, id store.Identity) string {
	t.Helper()
	raw, hash, err := newSessionToken()
	require.NoError(t, err)
	now := time.Now()
	_, err = st.CreateSession(context.Background(), store.NewSession{
		TokenHash:         hash,
		Subject:           "mount-s1",
		Email:             "alice@acme.com",
		Groups:            id.Groups,
		LastSeenAt:        now,
		ExpiresAt:         now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	return raw
}

// TestMountUnauthenticatedAPIReturns401 verifies that /api/projects rejects requests
// without a session cookie before reaching any handler.
func TestMountUnauthenticatedAPIReturns401(t *testing.T) {
	srv, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestMountAuthenticatedAPIReturns200 verifies that /api/projects returns 200 with a
// valid session cookie and includes the seeded project in the response.
func TestMountAuthenticatedAPIReturns200(t *testing.T) {
	srv, st, id := newMountServer(t)

	// Seed a project so the list returns something.
	p, err := st.CreateProject(context.Background(), id, "MountAlpha", "", "org")
	require.NoError(t, err)

	raw := seedSession(t, st, id)

	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.AddCookie(&http.Cookie{Name: srv.cfg.CookieName, Value: raw})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), p.ID.String())
}

// TestMountAuthLoginRedirects verifies that GET /auth/login issues a redirect.
func TestMountAuthLoginRedirects(t *testing.T) {
	srv, _, _ := newMountServer(t)
	mux := http.NewServeMux()
	srv.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)
}
