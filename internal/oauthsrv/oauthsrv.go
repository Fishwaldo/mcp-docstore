// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/giantswarm/mcp-oauth/handler"
	"github.com/giantswarm/mcp-oauth/providers/dex"
	"github.com/giantswarm/mcp-oauth/security"
	"github.com/giantswarm/mcp-oauth/server"
	"github.com/giantswarm/mcp-oauth/storage"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

// webClientID is the client_id of the first-party web SPA, seeded on every boot so it can
// authenticate against the embedded authorization server without a separate registration step.
const webClientID = "docstore-web"

// webClientScopes are the scopes granted to the first-party web SPA client.
var webClientScopes = []string{"openid", "profile", "email", "groups", "offline_access"}

// perIPRate and perIPBurst bound the token-bucket rate limiter applied to every request by
// source IP: 10 requests/second sustained with bursts up to 30.
const (
	perIPRate  = 10
	perIPBurst = 30
)

// Config carries everything the authorization server needs, mapped by the caller from the
// application config (cmd/server does this in one place).
type Config struct {
	PublicURL               string // AS issuer; also the base for {PublicURL}/mcp resource identifier
	UpstreamIssuer          string // upstream OIDC IdP (e.g. Okta org URL)
	UpstreamClientID        string
	UpstreamClientSecret    string
	UpstreamScopes          []string       // default [openid profile email groups]
	AllowPrivateIP          bool           // permit upstream issuers resolving to RFC-1918 addrs (SSRF opt-out)
	AllowPrivateIPRedirects bool           // permit client redirect URIs resolving to RFC-1918 addrs (DNS-rebinding opt-out)
	RootCAs                 *x509.CertPool // nil = system pool; for internal-CA IdPs
	DiscoveryTimeout        time.Duration
	AccessTokenTTL          time.Duration
	RefreshTokenTTL         time.Duration

	RegistrationOpen      bool     // true: open public DCR; false: allowlist below
	RegistrationAllowlist []string // exact-match HTTPS redirect URIs admitted to DCR when !RegistrationOpen

	// EnableClientManagement turns on the RFC 7592 client-management endpoints so DCR
	// responses carry a registration_access_token + registration_client_uri and clients can
	// update/delete their own registration. Sourced from oauth.enable_client_management.
	EnableClientManagement bool

	TrustProxy        bool
	TrustedProxyCount int

	// CookieSecure marks the consent-gate's cookies (ds_oauth_consent, ds_oauth_csrf; see
	// consent.go) as Secure (HTTPS-only). Sourced from the oauth.cookie_secure config key.
	CookieSecure bool
}

// Service bundles the assembled authorization server and its HTTP handler. Mount (mount.go)
// registers the handler's routes; PublicKeys feeds the in-process JWT verifier that validates
// the access tokens this server issues.
type Service struct {
	srv                  *server.Server
	h                    *handler.Handler
	km                   *KeyMaterial
	entc                 *ent.Client
	cfg                  Config
	logger               *slog.Logger
	rateLimiter          *security.RateLimiter
	clientRegRateLimiter *security.ClientRegistrationRateLimiter
}

// New assembles the embedded authorization server: an OIDC upstream (Dex-compatible generic
// provider) fronting an mcp-oauth server.Server that issues RFC 9068 JWT access tokens signed
// by km.Signer, backed by st (the ent-adapted storage.Combined).
func New(ctx context.Context, cfg Config, st storage.Combined, km *KeyMaterial, entc *ent.Client, logger *slog.Logger) (*Service, error) {
	if cfg.AllowPrivateIP {
		logger.Warn("upstream IdP SSRF protection relaxed",
			"reason", "config.oauth.upstream.allow_private_ip is true",
			"risk", "OIDC discovery and token requests may resolve to RFC-1918 or loopback addresses")
	}
	if cfg.AllowPrivateIPRedirects {
		logger.Warn("redirect URI SSRF protection relaxed",
			"reason", "config.oauth.allow_private_ip_redirects is true",
			"risk", "client redirect URIs may resolve to RFC-1918 or loopback addresses (DNS-rebinding protection off)")
	}

	provider, err := dex.NewProvider(&dex.Config{
		IssuerURL:      cfg.UpstreamIssuer,
		ClientID:       cfg.UpstreamClientID,
		ClientSecret:   cfg.UpstreamClientSecret,
		RedirectURL:    cfg.PublicURL + "/oauth/callback",
		Scopes:         cfg.UpstreamScopes,
		RequestTimeout: cfg.DiscoveryTimeout,
		Logger:         logger,
		AllowPrivateIP: cfg.AllowPrivateIP,
		RootCAs:        cfg.RootCAs,
	})
	if err != nil {
		return nil, fmt.Errorf("oauthsrv: construct upstream provider: %w", err)
	}

	var trustedRedirectURIs []string
	if !cfg.RegistrationOpen {
		trustedRedirectURIs = cfg.RegistrationAllowlist
	}

	rateLimiter := security.NewRateLimiter(perIPRate, perIPBurst, logger)
	clientRegRateLimiter := security.NewClientRegistrationRateLimiter(logger)

	srv, err := server.NewWithCombined(provider, st, &server.Config{
		Issuer:                                cfg.PublicURL,
		AccessTokenFormat:                     server.AccessTokenFormatJWT,
		AccessTokenSigningKey:                 km.Signer,
		AccessTokenSigningKeyID:               km.KID,
		AccessTokenSigningAlgorithm:           "ES256",
		AccessTokenTTL:                        int64(cfg.AccessTokenTTL.Seconds()),
		RefreshTokenTTL:                       int64(cfg.RefreshTokenTTL.Seconds()),
		ResourceIdentifier:                    cfg.PublicURL + "/mcp",
		SupportedScopes:                       []string{"openid", "profile", "email", "groups", "offline_access"},
		AllowPublicClientRegistration:         cfg.RegistrationOpen,
		AllowPrivateIPRedirectURIs:            cfg.AllowPrivateIPRedirects,
		TrustedPublicRegistrationRedirectURIs: trustedRedirectURIs,
		EnableClientManagementEndpoint:        cfg.EnableClientManagement,
		// RFC 8252 §7.3: native apps (Claude Code's ephemeral loopback callback among them)
		// register redirect URIs of the form http://127.0.0.1:PORT/... or
		// http://localhost:PORT/..., where the port is chosen at runtime. Without this the
		// library rejects every loopback redirect at registration, breaking native-app
		// onboarding. This does not loosen allowlist mode: the library rejects loopback
		// entries when normalizing TrustedPublicRegistrationRedirectURIs, so a loopback URI can
		// never be allowlisted, and allowlist-mode DCR is gated by that exact-match list first.
		AllowLocalhostRedirectURIs: true,
		EnableRevocationEndpoint:   true,
		TrustProxy:                 cfg.TrustProxy,
		TrustedProxyCount:          cfg.TrustedProxyCount,
	}, logger,
		server.WithRateLimiter(rateLimiter),
		server.WithClientRegistrationRateLimiter(clientRegRateLimiter),
	)
	if err != nil {
		rateLimiter.Stop()
		clientRegRateLimiter.Stop()
		return nil, fmt.Errorf("oauthsrv: construct authorization server: %w", err)
	}

	h := handler.New(srv, logger)

	return &Service{
		srv:                  srv,
		h:                    h,
		km:                   km,
		entc:                 entc,
		cfg:                  cfg,
		logger:               logger,
		rateLimiter:          rateLimiter,
		clientRegRateLimiter: clientRegRateLimiter,
	}, nil
}

// Close stops the background cleanup goroutines owned by the per-IP and client-registration
// rate limiters. Safe to call once during shutdown; the underlying limiters' Stop methods are
// themselves idempotent, so a repeat call is harmless.
func (s *Service) Close() {
	s.rateLimiter.Stop()
	s.clientRegRateLimiter.Stop()
}

// SeedWebClient idempotently registers the first-party web SPA as a PUBLIC OAuth client (no
// secret: the SPA authenticates via PKCE, per RFC 8252/OAuth 2.1 guidance for browser-based
// apps that cannot keep a secret confidential). Returns the client ID ("docstore-web").
//
// It is safe to call on every boot: if a client with this ID already exists and already has
// the desired shape (public type, "none" token endpoint auth method, matching redirect URIs),
// the record is left untouched so its updated_at does not churn on every restart. Otherwise
// the client is (re)saved — this also migrates a pre-existing confidential BFF client row left
// over from before the web SPA became a public client.
func (s *Service) SeedWebClient(ctx context.Context) (clientID string, err error) {
	redirectURIs := []string{s.cfg.PublicURL + "/auth/callback"}

	existing, err := s.srv.GetClient(ctx, webClientID)
	if err != nil && !errors.Is(err, storage.ErrClientNotFound) {
		return "", fmt.Errorf("oauthsrv: look up web client: %w", err)
	}
	if err == nil && webClientMatchesDesiredShape(existing, redirectURIs) {
		return webClientID, nil
	}

	client := &storage.Client{
		ClientID:                webClientID,
		ClientSecretHash:        "",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "DocStore Web UI",
		Scopes:                  webClientScopes,
	}
	if err := s.srv.SaveClient(ctx, client); err != nil {
		return "", fmt.Errorf("oauthsrv: save web client: %w", err)
	}

	return webClientID, nil
}

// webClientMatchesDesiredShape reports whether existing already has the public-client shape
// SeedWebClient wants, so a matching row can be left alone instead of rewritten on every boot.
func webClientMatchesDesiredShape(existing *storage.Client, redirectURIs []string) bool {
	return existing.ClientType == storage.ClientTypePublic &&
		existing.TokenEndpointAuthMethod == "none" &&
		slices.Equal(existing.RedirectURIs, redirectURIs)
}

// PublicKeys returns the JWT verification key(s) for in-process validation. In JWT access-token
// mode this is exactly one EC key matching km.Signer.Public().
func (s *Service) PublicKeys() ([]crypto.PublicKey, error) {
	set, err := s.srv.PublicJWKS()
	if err != nil {
		return nil, fmt.Errorf("oauthsrv: fetch public JWKS: %w", err)
	}

	keys := make([]crypto.PublicKey, 0, len(set.Keys))
	for _, k := range set.Keys {
		keys = append(keys, k.Key)
	}
	return keys, nil
}
