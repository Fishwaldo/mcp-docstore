// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSPAHandlerServesIndex(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h, err := srv.SPAHandler()
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "DocStore")
}

func TestSPAHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h, _ := srv.SPAHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/123/some-client-route", nil))
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "DocStore") // index.html, not 404
}

func TestSPAHandlerSetsCSP(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h, _ := srv.SPAHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "default-src 'self'")
	require.Contains(t, csp, "script-src 'self'")
	require.Contains(t, csp, "frame-ancestors 'none'")
	require.Contains(t, csp, "object-src 'none'")
}
