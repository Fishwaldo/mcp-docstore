// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadValidConfig(t *testing.T) {
	path := writeTemp(t, `
listen_addr: ":8080"
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database:
  driver: sqlite
  dsn: "file:test?mode=memory&cache=shared"
snapshot_retention: 5
oidc:
  issuer: "https://issuer.example.com"
  audience: "mcp-docstore"
tenants:
  - key: acme
    name: "Acme Corp"
    match:
      domains: ["acme.com", "ACME.io"]
      emails: ["contractor@gmail.com"]
  - key: globex
    name: "Globex"
    match:
      domains: ["globex.com"]
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.ListenAddr)
	require.Equal(t, "sqlite", cfg.Database.Driver)
	require.Equal(t, 5, cfg.SnapshotRetention)
	require.Len(t, cfg.Tenants, 2)
	// domains are normalized to lowercase
	require.Contains(t, cfg.Tenants[0].Match.Domains, "acme.io")
}

func TestLoadParsesAdminsAndPublicURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: { driver: sqlite, dsn: "file:t?mode=memory" }
oidc: { issuer: "https://idp.example.com", audience: "mcp-docstore" }
tenants:
  - key: acme
    name: Acme
    match: { domains: ["acme.com"] }
    admins: ["Alice@ACME.com"]
`), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "https://docs.example.com", cfg.PublicURL)
	require.Equal(t, []string{"alice@acme.com"}, cfg.Tenants[0].Admins) // normalize() lower-cases admins
}

func TestEmailVerifiedPolicyDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: a
    name: A
    match: {domains: ["a.com"]}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "require", cfg.OIDC.EmailVerifiedPolicy) // secure default
}

func TestEmailVerifiedPolicyAcceptsKnownValues(t *testing.T) {
	for _, v := range []string{"require", "if_present", "off"} {
		path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  audience: "mcp-docstore"
  email_verified_policy: "`+v+`"
tenants:
  - key: a
    name: A
    match: {domains: ["a.com"]}
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, v, cfg.OIDC.EmailVerifiedPolicy)
	}
}

func TestEmailVerifiedPolicyRejectsUnknownValue(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  audience: "mcp-docstore"
  email_verified_policy: "maybe"
tenants:
  - key: a
    name: A
    match: {domains: ["a.com"]}
`)
	_, err := Load(path)
	require.ErrorContains(t, err, "oidc.email_verified_policy")
}

func TestMaxRequestBytesDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: a
    name: A
    match: {domains: ["a.com"]}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, int64(4<<20), cfg.MaxRequestBytes) // default 4 MiB
}

func TestValidateRejectsNonPositiveSessionTimeout(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"zero", "session_timeout: 0\n"},
		{"negative", "session_timeout: -5s\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
`+tc.val)
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, "session_timeout")
		})
	}
}

func TestValidateRejectsNonPositiveMaxRequestBytes(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"zero", "max_request_bytes: 0\n"},
		{"negative", "max_request_bytes: -1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
`+tc.val)
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, "max_request_bytes")
		})
	}
}

func TestMaxRequestBytesPositiveOK(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
max_request_bytes: 1048576
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, int64(1048576), cfg.MaxRequestBytes)
}

func TestSessionTimeoutPositiveOK(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
session_timeout: 30s
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, cfg.SessionTimeout)
}

func TestValidateRejectsDuplicateDomain(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: a
    name: A
    match: {domains: ["dup.com"]}
  - key: b
    name: B
    match: {domains: ["dup.com"]}
`)
	_, err := Load(path)
	require.ErrorContains(t, err, "dup.com")
}

func TestSnapshotRetentionDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: a
    name: A
    match: {domains: ["a.com"]}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 10, cfg.SnapshotRetention) // default
}

func TestValidateErrorPaths(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name: "missing database.driver",
			yaml: `
database:
  dsn: "file:test"
`,
			wantSub: "database.driver is required",
		},
		{
			name: "missing database.dsn",
			yaml: `
database:
  driver: sqlite
`,
			wantSub: "database.dsn is required",
		},
		{
			name: "missing oidc.issuer",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {audience: "mcp-docstore"}
`,
			wantSub: "oidc.issuer is required",
		},
		{
			name: "missing oidc.audience",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com"}
`,
			wantSub: "oidc.audience is required",
		},
		{
			name: "missing public_url",
			yaml: `
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
`,
			wantSub: "public_url is required",
		},
		{
			name: "missing bleve_index_path",
			yaml: `
public_url: "https://docs.example.com"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
`,
			wantSub: "bleve_index_path is required",
		},
		{
			name: "empty tenant key",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: ""
    name: NoKey
    match: {domains: ["nokey.com"]}
`,
			wantSub: "tenant with empty key",
		},
		{
			name: "duplicate tenant key",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: alpha
    name: Alpha
    match: {domains: ["alpha.com"]}
  - key: alpha
    name: AlphaDup
    match: {domains: ["alpha2.com"]}
`,
			wantSub: `duplicate tenant key`,
		},
		{
			name: "duplicate email across tenants",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
tenants:
  - key: t1
    name: T1
    match: {emails: ["shared@example.com"]}
  - key: t2
    name: T2
    match: {emails: ["shared@example.com"]}
`,
			wantSub: "shared@example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, tc.yaml)
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantSub)
		})
	}
}
