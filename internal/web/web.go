// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"log/slog"
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// Config holds the BFF's OAuth client settings and session policy. Defaults (cookie name,
// timeouts) are applied by the caller that builds it from the app config.
type Config struct {
	ClientID              string
	ClientSecret          string
	RedirectURL           string
	PostLogoutRedirectURL string
	Scopes                []string
	CookieName            string
	CookieSecure          bool
	IdleTimeout           time.Duration
	AbsoluteTimeout       time.Duration
	SweepInterval         time.Duration
}

// Server holds the BFF dependencies. It is transport-only: all data access goes through
// the store/app layers.
type Server struct {
	cfg      Config
	store    *store.Store
	svc      *app.Service
	resolver *tenant.Resolver
	oidc     authClient
	log      *slog.Logger
}

// New constructs a BFF Server.
func New(cfg Config, st *store.Store, svc *app.Service, resolver *tenant.Resolver, oidc authClient, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "ds_session"
	}
	return &Server{cfg: cfg, store: st, svc: svc, resolver: resolver, oidc: oidc, log: log}
}
