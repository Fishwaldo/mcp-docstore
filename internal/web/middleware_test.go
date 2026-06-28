// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func TestRequireSessionNoCookie401(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireSessionValidPutsIdentityInContext(t *testing.T) {
	srv, st := newTestServer(t, nil)
	ctx := context.Background()
	now := time.Now()
	raw, hash, _ := newSessionToken()
	_, err := st.CreateSession(ctx, store.NewSession{
		TokenHash: hash, Subject: "s1", Email: "alice@acme.com", Groups: []string{"eng"},
		LastSeenAt: now, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	require.NoError(t, err)

	var gotOK bool
	var gotID store.Identity
	h := srv.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, gotOK = IdentityFromContext(r.Context())
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: srv.cfg.CookieName, Value: raw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	require.True(t, gotOK)
	require.NotEqual(t, uuid.Nil, gotID.UserID)
	require.Equal(t, []string{"eng"}, gotID.Groups)
}

func TestRequireSessionExpired401(t *testing.T) {
	srv, st := newTestServer(t, nil)
	ctx := context.Background()
	now := time.Now()
	raw, hash, _ := newSessionToken()
	_, err := st.CreateSession(ctx, store.NewSession{
		TokenHash: hash, Subject: "s1", Email: "alice@acme.com",
		LastSeenAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), AbsoluteExpiresAt: now.Add(-time.Hour),
	})
	require.NoError(t, err)
	h := srv.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: srv.cfg.CookieName, Value: raw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
