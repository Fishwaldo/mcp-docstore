// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readBodyHandler reads the whole body and maps a MaxBytesReader overflow to 413,
// mirroring how a real downstream handler surfaces the cap.
func readBodyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestMaxBytesCapsRequestBody(t *testing.T) {
	h := maxBytes(64, readBodyHandler())

	// Under the cap: the body reads cleanly, handler returns 200.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 32))))
	if rec.Code != http.StatusOK {
		t.Fatalf("small body: got %d, want %d", rec.Code, http.StatusOK)
	}

	// Over the cap: MaxBytesReader fails the read, handler returns 413.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 256))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body: got %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
