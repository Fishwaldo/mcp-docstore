// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompressCompressesLargeResponses(t *testing.T) {
	body := strings.Repeat("hello world ", 500) // > default min-size, highly compressible
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
	h, err := compress(inner)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	gr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	got, err := io.ReadAll(gr)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
}

func TestCompressSkipsMCPStream(t *testing.T) {
	// The MCP endpoint holds long-lived SSE streams that must flush event-by-event;
	// compression would buffer them and break streaming, so /mcp is never compressed
	// even when the client offers gzip.
	body := strings.Repeat("data: ping\n\n", 500)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	})
	h, err := compress(inner)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, body, rec.Body.String())
}

func TestCompressHonoursMissingAcceptEncoding(t *testing.T) {
	body := strings.Repeat("hello world ", 500)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
	h, err := compress(inner)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, body, rec.Body.String())
}
