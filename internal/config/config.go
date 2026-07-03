// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package config loads and validates the server configuration from a YAML file.
package config

import (
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	PublicURL         string `mapstructure:"public_url"`
	ListenAddr        string `mapstructure:"listen_addr"`
	SnapshotRetention int    `mapstructure:"snapshot_retention"`
	BleveIndexPath    string `mapstructure:"bleve_index_path"`
	// SessionTimeout reaps idle Streamable HTTP sessions after this duration with no
	// requests, bounding per-session bookkeeping. Must be positive; a zero or negative
	// value is rejected by Validate (Load defaults it to 2m when unset).
	SessionTimeout time.Duration `mapstructure:"session_timeout"`
	// MaxRequestBytes caps the request body size accepted on the MCP endpoint, via
	// http.MaxBytesReader, to bound memory on a single request. Defaults to 4 MiB.
	MaxRequestBytes int64        `mapstructure:"max_request_bytes"`
	Database        Database     `mapstructure:"database"`
	OIDC            OIDC         `mapstructure:"oidc"`
	OAuth           OAuth        `mapstructure:"oauth"`
	Tenants         []TenantSpec `mapstructure:"tenants"`
	Logging         Logging      `mapstructure:"logging"`
	// Web configures the optional BFF for web UI sessions. When nil, the web UI is disabled.
	Web *WebConfig `mapstructure:"web"`

	// RootCAPool is derived from OIDC.RootCA by Load and is not read from YAML directly.
	// It is nil unless oidc.root_ca names a file, in which case Load has already read and
	// parsed it into a cert pool for the upstream IdP's TLS verification.
	RootCAPool *x509.CertPool `mapstructure:"-"`
}

type Database struct {
	Driver string `mapstructure:"driver"` // sqlite | mysql | postgres
	DSN    string `mapstructure:"dsn"`
}

// OIDC configures the upstream identity provider the authorization server federates
// login to. The server itself is the OAuth issuer (see OAuth); this block is only the
// upstream login leg used during the /oauth/authorize -> upstream -> /oauth/callback hop.
type OIDC struct {
	Issuer       string `mapstructure:"issuer"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	// Scopes are requested from the upstream IdP during login. Default
	// [openid profile email groups offline_access]. offline_access is required for the
	// upstream to issue a refresh token; without it token refresh cannot work and the AS
	// falls back to full re-authentication when its cached provider token lapses.
	Scopes []string `mapstructure:"scopes"`
	// AllowPrivateIP permits the upstream issuer/discovery/token endpoints to resolve to
	// RFC-1918 or loopback addresses, relaxing the default SSRF protection. Only set this
	// for an internal IdP reachable solely on a private network; doing so is logged as a
	// warning at startup.
	AllowPrivateIP bool `mapstructure:"allow_private_ip"`
	// RootCA, when set, is a PEM file path of additional CA certificates trusted for the
	// upstream IdP's TLS connections (discovery, token, JWKS). Use for an internal IdP
	// whose certificate isn't signed by a public CA. Load parses this into RootCAPool.
	RootCA string `mapstructure:"root_ca"`
	// DiscoveryTimeout bounds the HTTP calls for OIDC discovery and JWKS key refresh against
	// the upstream IdP, so a hung or slow IdP can't block startup indefinitely. Default 15s.
	DiscoveryTimeout time.Duration `mapstructure:"discovery_timeout"`
	// Audience is unused; it exists only so Validate can detect a stale pre-authorization-
	// server config file (from before the server became its own OAuth issuer) and fail with
	// a pointer to the new format instead of silently mis-authenticating.
	Audience string `mapstructure:"audience"`
}

// OAuth configures the embedded authorization server, which is always on: this server is
// its own OAuth issuer, federating login to the upstream IdP configured by OIDC.
type OAuth struct {
	// AccessTokenTTL is the lifetime of issued access tokens. Default 1h.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl"`
	// RefreshTokenTTL is the lifetime of issued refresh tokens. Default 720h (30 days).
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
	// Registration controls dynamic client registration (RFC 7591): "open" (default) admits
	// any client; "allowlist" restricts registration to redirect URIs named in
	// RegistrationAllowlist.
	Registration string `mapstructure:"registration"`
	// RegistrationAllowlist lists the exact-match https:// redirect URIs admitted to dynamic
	// client registration when Registration is "allowlist". Ignored otherwise.
	RegistrationAllowlist []string `mapstructure:"registration_allowlist"`
	// TrustProxy, when true, trusts proxy-supplied forwarding headers (e.g.
	// X-Forwarded-For/Proto) for up to TrustedProxyCount hops, so the AS can compute the
	// correct client IP and scheme behind a reverse proxy or load balancer.
	TrustProxy bool `mapstructure:"trust_proxy"`
	// TrustedProxyCount is the number of trusted proxy hops to peel off forwarding headers
	// when TrustProxy is true. Default 1.
	TrustedProxyCount int `mapstructure:"trusted_proxy_count"`
}

// Logging configures the slog output. Level is debug|info|warn|error; Format is json|text.
// ClientIPHeader, when set (e.g. "X-Forwarded-For"), is the request header trusted for the
// caller's IP behind a proxy; empty means use the connection's RemoteAddr.
type Logging struct {
	Level          string `mapstructure:"level"`
	Format         string `mapstructure:"format"`
	ClientIPHeader string `mapstructure:"client_ip_header"`
}

type TenantSpec struct {
	Key    string      `mapstructure:"key"`
	Name   string      `mapstructure:"name"`
	Match  TenantMatch `mapstructure:"match"`
	Admins []string    `mapstructure:"admins"`
}

type TenantMatch struct {
	Domains []string `mapstructure:"domains"`
	Emails  []string `mapstructure:"emails"`
}

// WebConfig configures the optional BFF for web UI sessions. The BFF is a first-party
// client of the embedded authorization server (auto-seeded on boot), so it carries no
// OAuth client credentials or redirect/scope configuration of its own.
type WebConfig struct {
	// CookieSecure marks the session and CSRF cookies as Secure (HTTPS-only). It is a
	// pointer so an unset value defaults to true (secure by default); set it to false
	// only to opt out for local plain-HTTP development.
	CookieSecure    *bool         `mapstructure:"cookie_secure"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	AbsoluteTimeout time.Duration `mapstructure:"absolute_timeout"`
	SweepInterval   time.Duration `mapstructure:"sweep_interval"`
}

// Load reads, defaults, normalizes, and validates the config at path.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("snapshot_retention", 10)
	v.SetDefault("session_timeout", 2*time.Minute)
	v.SetDefault("max_request_bytes", 4<<20)
	v.SetDefault("oidc.scopes", []string{"openid", "profile", "email", "groups", "offline_access"})
	v.SetDefault("oidc.discovery_timeout", 15*time.Second)
	v.SetDefault("oauth.access_token_ttl", time.Hour)
	v.SetDefault("oauth.refresh_token_ttl", 720*time.Hour)
	v.SetDefault("oauth.registration", "open")
	v.SetDefault("oauth.trusted_proxy_count", 1)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalize()
	cfg.applyDefaults()
	if err := cfg.loadRootCA(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) normalize() {
	for i := range c.Tenants {
		m := &c.Tenants[i].Match
		for j, d := range m.Domains {
			m.Domains[j] = strings.ToLower(strings.TrimSpace(d))
		}
		for j, e := range m.Emails {
			m.Emails[j] = strings.ToLower(strings.TrimSpace(e))
		}
	}
	for i := range c.Tenants {
		for j, a := range c.Tenants[i].Admins {
			c.Tenants[i].Admins[j] = strings.ToLower(strings.TrimSpace(a))
		}
	}
}

func (c *Config) applyDefaults() {
	if c.Web != nil {
		if c.Web.IdleTimeout <= 0 {
			c.Web.IdleTimeout = 24 * time.Hour
		}
		if c.Web.AbsoluteTimeout <= 0 {
			c.Web.AbsoluteTimeout = 168 * time.Hour
		}
		if c.Web.SweepInterval <= 0 {
			c.Web.SweepInterval = 1 * time.Hour
		}
		if c.Web.CookieSecure == nil {
			secure := true
			c.Web.CookieSecure = &secure
		}
	}
}

// loadRootCA reads and parses OIDC.RootCA, if set, into RootCAPool. A configured path that
// is missing or contains no valid PEM certificate fails Load outright, since a silently
// empty pool would fall back to the system trust store and mask a misconfiguration.
func (c *Config) loadRootCA() error {
	if c.OIDC.RootCA == "" {
		return nil
	}
	pemBytes, err := os.ReadFile(c.OIDC.RootCA)
	if err != nil {
		return fmt.Errorf("read oidc.root_ca %q: %w", c.OIDC.RootCA, err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pemBytes); !ok {
		return fmt.Errorf("oidc.root_ca %q: no valid PEM certificate found", c.OIDC.RootCA)
	}
	c.RootCAPool = pool
	return nil
}

// Validate enforces structural rules and the uniqueness guarantee that no domain or
// email maps to more than one tenant (so a caller can never resolve to two tenants).
// It assumes normalize() has already been applied (as Load does), so domain
// and email values have been lower-cased before duplicate detection runs.
func (c *Config) Validate() error {
	if c.Database.Driver == "" {
		return fmt.Errorf("database.driver is required")
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
	}
	if c.OIDC.Issuer == "" {
		return fmt.Errorf("oidc.issuer is required")
	}
	// The stale-audience guard must run before the client_id/client_secret checks: a genuine
	// pre-authorization-server config has issuer+audience but never had client_id/client_secret,
	// so checking those first would mask the actionable migration message with a generic
	// "client_id is required" error.
	if c.OIDC.Audience != "" {
		return fmt.Errorf("oidc.audience is no longer used: the server is now its own OAuth issuer; see config.example.yaml")
	}
	if c.OIDC.ClientID == "" {
		return fmt.Errorf("oidc.client_id is required")
	}
	if c.OIDC.ClientSecret == "" {
		return fmt.Errorf("oidc.client_secret is required")
	}
	if c.PublicURL == "" {
		return fmt.Errorf("public_url is required")
	}
	if c.BleveIndexPath == "" {
		return fmt.Errorf("bleve_index_path is required")
	}
	if c.SessionTimeout <= 0 {
		return fmt.Errorf("session_timeout must be positive (got %s)", c.SessionTimeout)
	}
	if c.MaxRequestBytes <= 0 {
		return fmt.Errorf("max_request_bytes must be positive (got %d)", c.MaxRequestBytes)
	}
	if c.OIDC.DiscoveryTimeout <= 0 {
		return fmt.Errorf("oidc.discovery_timeout must be positive (got %s)", c.OIDC.DiscoveryTimeout)
	}
	switch c.OAuth.Registration {
	case "open":
	case "allowlist":
		if len(c.OAuth.RegistrationAllowlist) == 0 {
			return fmt.Errorf("oauth.registration_allowlist must have at least one entry when oauth.registration is \"allowlist\"")
		}
		for _, u := range c.OAuth.RegistrationAllowlist {
			if !strings.HasPrefix(u, "https://") {
				return fmt.Errorf("oauth.registration_allowlist entry %q must be an https:// URL", u)
			}
		}
	default:
		return fmt.Errorf("oauth.registration must be one of open|allowlist, got %q", c.OAuth.Registration)
	}
	if c.OAuth.TrustedProxyCount < 0 {
		return fmt.Errorf("oauth.trusted_proxy_count must be >= 0 (got %d)", c.OAuth.TrustedProxyCount)
	}
	seenKey := map[string]bool{}
	seenDomain := map[string]string{}
	seenEmail := map[string]string{}
	for _, t := range c.Tenants {
		if t.Key == "" {
			return fmt.Errorf("tenant with empty key")
		}
		if seenKey[t.Key] {
			return fmt.Errorf("duplicate tenant key %q", t.Key)
		}
		seenKey[t.Key] = true
		for _, d := range t.Match.Domains {
			if other, ok := seenDomain[d]; ok {
				return fmt.Errorf("domain %q mapped to both %q and %q", d, other, t.Key)
			}
			seenDomain[d] = t.Key
		}
		for _, e := range t.Match.Emails {
			if other, ok := seenEmail[e]; ok {
				return fmt.Errorf("email %q mapped to both %q and %q", e, other, t.Key)
			}
			seenEmail[e] = t.Key
		}
	}
	switch c.Logging.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be one of debug|info|warn|error, got %q", c.Logging.Level)
	}
	switch c.Logging.Format {
	case "", "json", "text":
	default:
		return fmt.Errorf("logging.format must be json or text, got %q", c.Logging.Format)
	}
	return nil
}
