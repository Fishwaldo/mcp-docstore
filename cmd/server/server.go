// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package server wires the configured layers into a running MCP server and hosts the
// CLI subcommands.
package server

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/giantswarm/mcp-oauth/security"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/assets"
	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	imcp "github.com/Fishwaldo/mcp-docstore/internal/mcp"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/entstore"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
	"github.com/Fishwaldo/mcp-docstore/internal/web"
)

const metadataPath = "/.well-known/oauth-protected-resource"

// Version is the build version advertised in the MCP initialize handshake. Release builds
// stamp it via -ldflags "-X github.com/Fishwaldo/mcp-docstore/cmd/server.Version=v1.2.3";
// unstamped builds fall back to the module version the toolchain embeds, then to "dev".
var Version = "dev"

func resolveVersion() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// Run loads config and either serves HTTP (no subcommand / "serve") or runs a subcommand
// ("rebuild-index"). It returns when ctx is cancelled (graceful shutdown) or on error.
func Run(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("mcp-docstore", flag.ContinueOnError)
	cfgPath := fs.String("config", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	logger = newLogger(os.Stderr, cfg.Logging)

	st, err := store.Open(cfg.Database.Driver, cfg.Database.DSN, store.WithSnapshotRetention(cfg.SnapshotRetention))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	for _, ts := range cfg.Tenants { // tenants exist only as declared in config
		if _, err := st.EnsureTenant(ctx, ts.Key, ts.Name); err != nil {
			return fmt.Errorf("seed tenant %q: %w", ts.Key, err)
		}
	}

	idx, err := search.Open(cfg.BleveIndexPath)
	if err != nil {
		return err
	}
	defer idx.Close()
	idxSvc := index.New(st, idx)

	switch fs.Arg(0) {
	case "rebuild-index":
		logger.Info("rebuilding search index")
		return idxSvc.RebuildAll(ctx)
	case "", "serve":
		// continue to serve
	default:
		return fmt.Errorf("unknown subcommand %q", fs.Arg(0))
	}

	empty, err := idx.IsEmpty()
	if err != nil {
		return err
	}
	if empty { // first boot with an empty index: build it from the database
		logger.Info("search index empty; building from database")
		if err := idxSvc.RebuildAll(ctx); err != nil {
			return err
		}
	}

	resolver, err := tenant.NewResolver(cfg.Tenants)
	if err != nil {
		return err
	}

	entc := st.EntClient()
	km, err := oauthsrv.LoadOrCreateKeyMaterial(ctx, entc)
	if err != nil {
		return err
	}
	enc, err := security.NewEncryptor(km.EncryptionKey)
	if err != nil {
		return err
	}
	ost := entstore.New(entc, enc, 24*time.Hour)

	// cookieSecure governs the AS's consent cookie. config.Load defaults cfg.OAuth.CookieSecure
	// to true, so this only ever turns false via an explicit operator opt-out.
	cookieSecure := true
	if cfg.OAuth.CookieSecure != nil {
		cookieSecure = *cfg.OAuth.CookieSecure
	}
	if !cookieSecure {
		logger.Warn("oauth cookie_secure is false; the consent cookie will be sent over plain HTTP — use only for local development")
	}

	asvc, err := oauthsrv.New(ctx, oauthsrv.Config{
		PublicURL:               cfg.PublicURL,
		UpstreamIssuer:          cfg.OIDC.Issuer,
		UpstreamClientID:        cfg.OIDC.ClientID,
		UpstreamClientSecret:    cfg.OIDC.ClientSecret,
		UpstreamScopes:          cfg.OIDC.Scopes,
		AllowPrivateIP:          cfg.OIDC.AllowPrivateIP,
		AllowPrivateIPRedirects: cfg.OAuth.AllowPrivateIPRedirects,
		RootCAs:                 cfg.RootCAPool,
		DiscoveryTimeout:        cfg.OIDC.DiscoveryTimeout,
		AccessTokenTTL:          cfg.OAuth.AccessTokenTTL,
		RefreshTokenTTL:         cfg.OAuth.RefreshTokenTTL,
		RegistrationOpen:        cfg.OAuth.Registration == "open",
		RegistrationAllowlist:   cfg.OAuth.RegistrationAllowlist,
		TrustProxy:              cfg.OAuth.TrustProxy,
		TrustedProxyCount:       cfg.OAuth.TrustedProxyCount,
		CookieSecure:            cookieSecure,
	}, ost, km, entc, logger)
	if err != nil {
		return err
	}
	defer asvc.Close()

	keys, err := asvc.PublicKeys()
	if err != nil {
		return err
	}
	verifier := auth.NewLocalVerifier(cfg.PublicURL, []string{cfg.PublicURL + "/mcp", cfg.PublicURL}, keys, ost)

	svc := app.NewService(st, idxSvc, logger)
	icons := []sdkmcp.Icon{
		{Source: cfg.PublicURL + "/icon-512.png", MIMEType: "image/png", Sizes: []string{"512x512"}},
		{Source: cfg.PublicURL + "/icon-96.png", MIMEType: "image/png", Sizes: []string{"96x96"}},
	}
	mcpServer := imcp.NewMCPServer(svc, auth.IdentityFromRequest, logger, icons, resolveVersion())

	bearer := mcpauth.RequireBearerToken(
		auth.NewResourceVerifier(verifier, resolver, st, logger, cfg.Logging.ClientIPHeader),
		&mcpauth.RequireBearerTokenOptions{ResourceMetadataURL: cfg.PublicURL + metadataPath},
	)
	streamable := sdkmcp.NewStreamableHTTPHandler(
		func(*http.Request) *sdkmcp.Server { return mcpServer },
		&sdkmcp.StreamableHTTPOptions{SessionTimeout: cfg.SessionTimeout, Logger: logger},
	)

	mux := http.NewServeMux()
	mux.Handle("/icon-512.png", servePNG(assets.Icon512PNG))
	mux.Handle("/icon-96.png", servePNG(assets.Icon96PNG))
	mcpHandler := logRequests(logger, cfg.Logging.ClientIPHeader, bearer(streamable))
	mux.Handle("/mcp", maxBytes(cfg.MaxRequestBytes, mcpHandler))

	// asvc.Mount serves the RFC 9728 protected-resource metadata (formerly the go-sdk's
	// ProtectedResourceMetadataHandler, registered directly at metadataPath), RFC 8414
	// authorization-server metadata, the JWKS, and every /oauth/* endpoint. The embedded
	// authorization server is always on, independent of cfg.Web.
	asvc.Mount(mux)

	go sweepOAuthStore(ctx, logger, ost, cfg.OAuth.SweepInterval)

	if cfg.Web.Enabled {
		if _, err := asvc.SeedWebClient(ctx); err != nil {
			return err
		}
		webSrv := web.New(web.Config{}, st, svc, resolver, verifier, logger)
		webSrv.Mount(mux)
		spa, err := webSrv.SPAHandler()
		if err != nil {
			return err
		}
		mux.Handle("/", spa)
		logger.Info("web UI enabled")
	}

	// ReadTimeout / WriteTimeout are deliberately NOT set: Streamable HTTP holds
	// long-lived SSE response streams, and a write deadline would sever them
	// mid-stream. ReadHeaderTimeout still defends against slow-header (slow-loris)
	// attacks, IdleTimeout reaps idle keep-alive conns, and MaxHeaderBytes caps
	// header memory. The request body is bounded per-route by maxBytes above.
	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()
	logger.Info("serving", "addr", cfg.ListenAddr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// sweepOAuthStore periodically deletes expired rows from the embedded authorization server's
// storage (authorization codes/states, refresh tokens, revoked JTIs, cached provider tokens and
// userinfo, aged-out revoked refresh-token families) until ctx is cancelled. It runs regardless
// of whether the web UI is enabled, since the authorization server itself is always on.
func sweepOAuthStore(ctx context.Context, logger *slog.Logger, ost *entstore.Store, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := ost.DeleteExpired(ctx, time.Now())
			if err != nil {
				logger.ErrorContext(ctx, "oauth store sweep failed", "error", err)
				continue
			}
			if n > 0 {
				logger.DebugContext(ctx, "swept expired oauth rows", "count", n)
			}
		}
	}
}

// servePNG returns a handler that serves the given PNG bytes at an unauthenticated route.
func servePNG(b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(b)
	}
}

// maxBytes caps the request body on the MCP route via http.MaxBytesReader, bounding
// memory consumed by a single request. When the limit is exceeded the body read fails
// and the request is rejected by the downstream handler. The cap is applied at the
// outermost layer so every downstream reader is bounded.
func maxBytes(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter to capture the response status code. It exposes
// Unwrap so http.ResponseController (used by the SDK's Streamable HTTP handler to Flush SSE
// streams) reaches the underlying writer.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// newLogger builds the slog logger from config: level (debug|info|warn|error) and format
// (json|text). config.Validate has already rejected invalid values, so unknown values fall
// back to info/json defensively.
func newLogger(w io.Writer, c config.Logging) *slog.Logger {
	var level slog.Level
	switch c.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if c.Format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// logRequests logs one transport event per HTTP request: method, path, status, client IP,
// and latency. It never logs the Authorization header. Successful requests log at DEBUG; a
// 4xx/5xx (including pre-auth 401s the MCP layer never sees) logs at WARN.
func logRequests(logger *slog.Logger, ipHeader string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if rec.status >= 400 {
			level = slog.LevelWarn
		}
		logger.LogAttrs(r.Context(), level, "http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.String("client_ip", auth.ClientIP(r, ipHeader)),
			slog.Int64("dur_ms", time.Since(start).Milliseconds()),
		)
	})
}
