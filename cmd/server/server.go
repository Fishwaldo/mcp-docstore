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
	for _, ts := range cfg.Tenants { // tenants are config-seeded (spec §3)
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
	if empty { // first boot with an empty index: build from the DB (spec §6)
		logger.Info("search index empty; building from database")
		if err := idxSvc.RebuildAll(ctx); err != nil {
			return err
		}
	}

	resolver, err := tenant.NewResolver(cfg.Tenants)
	if err != nil {
		return err
	}
	oidcVerifier, err := auth.NewOIDCVerifier(ctx, cfg.OIDC.Issuer, cfg.OIDC.Audience, cfg.OIDC.EmailClaim, cfg.OIDC.GroupsClaim)
	if err != nil {
		return err
	}

	svc := imcp.NewService(st, idxSvc, logger)
	icons := []sdkmcp.Icon{
		{Source: cfg.PublicURL + "/icon-512.png", MIMEType: "image/png", Sizes: []string{"512x512"}},
		{Source: cfg.PublicURL + "/icon-96.png", MIMEType: "image/png", Sizes: []string{"96x96"}},
	}
	mcpServer := imcp.NewMCPServer(svc, auth.IdentityFromRequest, logger, icons)

	bearer := mcpauth.RequireBearerToken(
		auth.NewResourceVerifier(oidcVerifier, resolver, st),
		&mcpauth.RequireBearerTokenOptions{ResourceMetadataURL: cfg.PublicURL + metadataPath},
	)
	streamable := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return mcpServer }, nil)

	mux := http.NewServeMux()
	mux.Handle(metadataPath, mcpauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.PublicURL,
		AuthorizationServers:   []string{cfg.OIDC.Issuer},
		BearerMethodsSupported: []string{"header"},
	}))
	mux.Handle("/icon-512.png", servePNG(assets.Icon512PNG))
	mux.Handle("/icon-96.png", servePNG(assets.Icon96PNG))
	mux.Handle("/", logRequests(logger, bearer(streamable)))

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

// logRequests is minimal slog request logging. It never logs the Authorization header.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).String())
	})
}
