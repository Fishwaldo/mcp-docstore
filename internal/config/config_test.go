package config

import (
	"os"
	"path/filepath"
	"testing"

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

func TestValidateRejectsDuplicateDomain(t *testing.T) {
	path := writeTemp(t, `
database: {driver: sqlite, dsn: "x"}
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
database: {driver: sqlite, dsn: "x"}
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
			name: "empty tenant key",
			yaml: `
database: {driver: sqlite, dsn: "x"}
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
database: {driver: sqlite, dsn: "x"}
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
database: {driver: sqlite, dsn: "x"}
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
