// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"net/http/httptest"
)

// muxTransport dispatches an HTTP request straight to h via httptest.NewRecorder, so the web
// BFF's server-to-server calls to the embedded authorization server (token exchange, revocation)
// never leave the process — the server never needs to dial its own public_url, which may not
// even be reachable from inside its own container or behind a load balancer that only terminates
// inbound traffic.
type muxTransport struct{ h http.Handler }

func (t muxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}
