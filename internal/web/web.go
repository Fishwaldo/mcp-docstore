// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"log/slog"

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// Config is currently empty: the web server has no policy of its own to hold. Authentication
// is delegated entirely to verifier (the same bearer-token pipeline the /mcp transport uses),
// so there is no cookie, timeout, or OAuth-client setting left to configure here. Kept as a
// struct rather than removed so New's signature is stable if the web server ever grows its
// own settings again.
type Config struct{}

// Server holds the web API's dependencies. It is transport-only: all data access goes
// through the store/app layers, and all authentication goes through verifier via RequireBearer.
type Server struct {
	cfg      Config
	store    *store.Store
	svc      *app.Service
	resolver *tenant.Resolver
	verifier auth.Verifier
	log      *slog.Logger
}

// New constructs a web Server. verifier is the same bearer-token verifier the /mcp transport
// authenticates with — RequireBearer runs requests through auth.VerifyRequestIdentity using it,
// resolver, and st, so /api and /mcp trust exactly the same tokens.
func New(cfg Config, st *store.Store, svc *app.Service, resolver *tenant.Resolver, verifier auth.Verifier, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{cfg: cfg, store: st, svc: svc, resolver: resolver, verifier: verifier, log: log}
}
