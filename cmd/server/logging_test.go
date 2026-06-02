// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/logtest"
)

func TestLogRequestsCapturesStatusAndLevel(t *testing.T) {
	logger, buf := logtest.New()

	// 200 -> DEBUG
	h := logRequests(logger, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.RemoteAddr = "203.0.113.9:5000"
	h.ServeHTTP(httptest.NewRecorder(), r)

	rec := logtest.Find(buf, "http_request")
	require.NotNil(t, rec)
	require.Equal(t, "DEBUG", rec["level"])
	require.Equal(t, float64(200), rec["status"])
	require.Equal(t, "203.0.113.9", rec["client_ip"])

	// 401 -> WARN
	logger2, buf2 := logtest.New()
	h2 := logRequests(logger2, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	h2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	rec2 := logtest.Find(buf2, "http_request")
	require.NotNil(t, rec2)
	require.Equal(t, "WARN", rec2["level"])
	require.Equal(t, float64(401), rec2["status"])
}
