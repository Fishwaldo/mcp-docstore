// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package config loads and validates the server configuration from a YAML file.
package config

import (
	"fmt"
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
	// requests, bounding per-session bookkeeping. Zero means sessions are never
	// reaped (the SDK default).
	SessionTimeout time.Duration `mapstructure:"session_timeout"`
	Database       Database      `mapstructure:"database"`
	OIDC           OIDC          `mapstructure:"oidc"`
	Tenants        []TenantSpec  `mapstructure:"tenants"`
	Logging        Logging       `mapstructure:"logging"`
}

type Database struct {
	Driver string `mapstructure:"driver"` // sqlite | mysql | postgres
	DSN    string `mapstructure:"dsn"`
}

type OIDC struct {
	Issuer string `mapstructure:"issuer"`
	// DiscoveryURL, when set, overrides the standard OIDC discovery location
	// (issuer + /.well-known/openid-configuration). Point it at a provider that
	// publishes its metadata document at an off-spec path — e.g. an RFC 8414
	// authorization-server metadata URL (/.well-known/oauth-authorization-server).
	// The document's "issuer" must match Issuer or startup fails.
	DiscoveryURL string `mapstructure:"discovery_url"`
	Audience     string `mapstructure:"audience"`
	EmailClaim   string `mapstructure:"email_claim"`
	GroupsClaim  string `mapstructure:"groups_claim"`
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

// Load reads, defaults, normalizes, and validates the config at path.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("snapshot_retention", 10)
	v.SetDefault("session_timeout", 2*time.Minute)
	v.SetDefault("oidc.email_claim", "email")
	v.SetDefault("oidc.groups_claim", "groups")
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
	if c.OIDC.Audience == "" {
		return fmt.Errorf("oidc.audience is required")
	}
	if c.PublicURL == "" {
		return fmt.Errorf("public_url is required")
	}
	if c.BleveIndexPath == "" {
		return fmt.Errorf("bleve_index_path is required")
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
