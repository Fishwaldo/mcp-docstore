// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireCSRFAllowsGET(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	require.Equal(t, 200, rec.Code)
}

func TestRequireCSRFRejectsMismatchedPOST(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	req.Header.Set("X-CSRF-Token", "DIFFERENT")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireCSRFAllowsMatchedPOST(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	h := srv.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
}
