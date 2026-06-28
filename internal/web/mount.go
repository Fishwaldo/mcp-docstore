// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Mount wires the BFF routes onto mux.
//
// Auth routes are plain handlers:
//   - GET /auth/login  → HandleLogin
//   - GET /auth/callback → HandleCallback
//   - GET /auth/logout → HandleLogout
//
// API routes sit behind RequireSession then RequireCSRF: /api/ dispatches to a
// dedicated sub-mux that carries the 9 read operations (registered in registerAPI)
// plus the Huma-generated OpenAPI spec and docs UI. Because humago.NewWithPrefix
// prepends "/api" to every operation path, operations registered without a prefix
// (e.g. "/projects") are served at "/api/projects" on the sub-mux — a request to
// "/api/projects" on the outer mux reaches the sub-mux with its full path intact,
// so no StripPrefix is needed. The OpenAPI spec (/api/openapi.json) and docs
// (/api/docs) are registered the same way and therefore also gated by RequireSession.
func (s *Server) Mount(mux *http.ServeMux) {
	// Auth endpoints — no middleware.
	mux.HandleFunc("GET /auth/login", s.HandleLogin)
	mux.HandleFunc("GET /auth/callback", s.HandleCallback)
	mux.HandleFunc("GET /auth/logout", s.HandleLogout)

	// API sub-mux: humago.NewWithPrefix registers every operation at /api<path>.
	apiMux := http.NewServeMux()
	cfg := huma.DefaultConfig("DocStore API", "1.0.0")
	api := humago.NewWithPrefix(apiMux, "/api", cfg)
	s.registerAPI(api)

	// Wrap the sub-mux: session auth first, CSRF second.
	mux.Handle("/api/", s.RequireSession(s.RequireCSRF(apiMux)))
}
