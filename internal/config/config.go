// Package config loads and validates the server configuration from a YAML file.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	ListenAddr        string       `mapstructure:"listen_addr"`
	SnapshotRetention int          `mapstructure:"snapshot_retention"`
	BleveIndexPath    string       `mapstructure:"bleve_index_path"`
	Database          Database     `mapstructure:"database"`
	OIDC              OIDC         `mapstructure:"oidc"`
	Tenants           []TenantSpec `mapstructure:"tenants"`
}

type Database struct {
	Driver string `mapstructure:"driver"` // sqlite | mysql | postgres
	DSN    string `mapstructure:"dsn"`
}

type OIDC struct {
	Issuer      string `mapstructure:"issuer"`
	Audience    string `mapstructure:"audience"`
	EmailClaim  string `mapstructure:"email_claim"`
	GroupsClaim string `mapstructure:"groups_claim"`
}

type TenantSpec struct {
	Key   string      `mapstructure:"key"`
	Name  string      `mapstructure:"name"`
	Match TenantMatch `mapstructure:"match"`
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
	v.SetDefault("oidc.email_claim", "email")
	v.SetDefault("oidc.groups_claim", "groups")

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
}

// Validate enforces structural rules and the spec §3 uniqueness guarantee:
// no domain or email may map to more than one tenant.
// It assumes normalize() has already been applied (as Load does), so domain
// and email values have been lower-cased before duplicate detection runs.
func (c *Config) Validate() error {
	if c.Database.Driver == "" {
		return fmt.Errorf("database.driver is required")
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
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
	return nil
}
