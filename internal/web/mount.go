// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Mount wires the web API routes onto mux.
//
// API routes sit behind RequireBearer: /api/ dispatches to a dedicated sub-mux that carries
// the read operations (registered in registerAPI) plus the Huma-generated OpenAPI document and
// docs UI. Because humago.NewWithPrefix prepends "/api" to every operation path, operations
// registered without a prefix (e.g. "/projects") are served at "/api/projects" on the sub-mux —
// a request to "/api/projects" on the outer mux reaches the sub-mux with its full path intact,
// so no StripPrefix is needed. The OpenAPI document (/api/openapi.json) and docs UI (/api/docs)
// are registered the same way; RequireBearer exempts exactly those two GET paths (see
// publicAPIPaths) so they render without a token.
//
// Mount also registers two root aliases, GET /openapi.json and GET /docs, that rewrite the
// request path to its /api/... equivalent and dispatch directly to apiMux — serving the exact
// same bytes as the /api originals, unauthenticated, without duplicating Huma's spec/docs
// handlers.
func (s *Server) Mount(mux *http.ServeMux) {
	apiMux := http.NewServeMux()
	cfg := huma.DefaultConfig("DocStore API", "1.0.0")
	api := humago.NewWithPrefix(apiMux, "/api", cfg)
	s.registerAPI(api)

	mux.Handle("/api/", s.RequireBearer(apiMux))

	mux.HandleFunc("GET /openapi.json", rootAlias(apiMux, "/api/openapi.json"))
	mux.HandleFunc("GET /docs", rootAlias(apiMux, "/api/docs"))
}

// rootAlias returns a handler that rewrites the incoming request's path to apiPath and
// dispatches it to apiMux, so a root-level request serves identical bytes to hitting apiPath
// directly under /api.
func rootAlias(apiMux *http.ServeMux, apiPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		r2.URL.Path = apiPath
		r2.RequestURI = apiPath
		apiMux.ServeHTTP(w, r2)
	}
}
