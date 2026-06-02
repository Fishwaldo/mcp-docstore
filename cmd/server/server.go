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
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/Fishwaldo/mcp-docstore/assets"
	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	imcp "github.com/Fishwaldo/mcp-docstore/internal/mcp"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
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
	oidcVerifier, err := auth.NewOIDCVerifier(ctx, cfg.OIDC.Issuer, cfg.OIDC.DiscoveryURL, cfg.OIDC.Audience, cfg.OIDC.EmailClaim, cfg.OIDC.GroupsClaim)
	if err != nil {
		return err
	}

	svc := imcp.NewService(st, idxSvc, logger)
	icons := []sdkmcp.Icon{
		{Source: cfg.PublicURL + "/icon-512.png", MIMEType: "image/png", Sizes: []string{"512x512"}},
		{Source: cfg.PublicURL + "/icon-96.png", MIMEType: "image/png", Sizes: []string{"96x96"}},
	}
	mcpServer := imcp.NewMCPServer(svc, auth.IdentityFromRequest, logger, icons, resolveVersion())

	bearer := mcpauth.RequireBearerToken(
		auth.NewResourceVerifier(oidcVerifier, resolver, st, logger, cfg.Logging.ClientIPHeader),
		&mcpauth.RequireBearerTokenOptions{ResourceMetadataURL: cfg.PublicURL + metadataPath},
	)
	streamable := sdkmcp.NewStreamableHTTPHandler(
		func(*http.Request) *sdkmcp.Server { return mcpServer },
		&sdkmcp.StreamableHTTPOptions{SessionTimeout: cfg.SessionTimeout, Logger: logger},
	)

	mux := http.NewServeMux()
	mux.Handle(metadataPath, mcpauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.PublicURL,
		AuthorizationServers:   []string{cfg.OIDC.Issuer},
		BearerMethodsSupported: []string{"header"},
	}))
	mux.Handle("/icon-512.png", servePNG(assets.Icon512PNG))
	mux.Handle("/icon-96.png", servePNG(assets.Icon96PNG))
	mux.Handle("/", logRequests(logger, cfg.Logging.ClientIPHeader, bearer(streamable)))

	httpSrv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}
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

// servePNG returns a handler that serves the given PNG bytes at an unauthenticated route.
func servePNG(b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(b)
	}
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
