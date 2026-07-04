// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"net/http"

	"github.com/giantswarm/mcp-oauth/handler"
)

// Mount registers every route the embedded authorization server serves onto mux: the OAuth
// flow endpoints and discovery documents from github.com/giantswarm/mcp-oauth, gated by the
// human consent check in consent.go for any client that is not our first-party web SPA
// (docstore-web), plus our own POST /oauth/consent endpoint that records that approval.
//
// The library's routes are registered on an inner *http.ServeMux so consentGate can intercept
// GET /oauth/authorize before it ever reaches them. Every other /oauth/* request (token,
// callback, register, revoke, introspect, ...) and everything under /.well-known/ (Protected
// Resource Metadata, Authorization Server Metadata, JWKS) is passed straight through
// unmodified — the gate only cares about the one route that can trigger an upstream login
// redirect on a client's behalf.
func (s *Service) Mount(mux *http.ServeMux) {
	inner := http.NewServeMux()
	s.h.RegisterOAuthRoutes(inner, handler.OAuthRoutesOptions{MCPPath: "/mcp", IncludeMetadata: true})

	mux.Handle("/oauth/", s.consentGate(inner))
	mux.Handle("/.well-known/", inner)
	mux.HandleFunc("POST /oauth/consent", s.handleConsentSubmit)
}
