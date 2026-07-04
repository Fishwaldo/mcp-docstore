// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
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

// writeTestCACertPEM generates a throwaway self-signed EC certificate and writes it as a
// PEM file, returning the path. Used to exercise Load's oidc.root_ca parsing without
// depending on any real CA material.
func writeTestCACertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Test CA"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return path
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
  client_id: "test-client"
  client_secret: "test-secret"
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
oidc: { issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret" }
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

// TestLoadDefaults exercises the full default set introduced by the embedded
// authorization server config rework: OIDC upstream scopes, OAuth token TTLs,
// registration mode, and trusted proxy count.
func TestLoadDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, []string{"openid", "profile", "email", "groups", "offline_access"}, cfg.OIDC.Scopes)
	require.Equal(t, 15*time.Second, cfg.OIDC.DiscoveryTimeout)
	require.Equal(t, 15*time.Minute, cfg.OAuth.AccessTokenTTL)
	require.Equal(t, 168*time.Hour, cfg.OAuth.RefreshTokenTTL)
	require.Equal(t, "open", cfg.OAuth.Registration)
	require.Equal(t, 1, cfg.OAuth.TrustedProxyCount)
}

func TestMaxRequestBytesDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`+tc.val)
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, "max_request_bytes")
		})
	}
}

func TestValidateRejectsNonPositiveDiscoveryTimeout(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"zero", "  discovery_timeout: 0\n"},
		{"negative", "  discovery_timeout: -1s\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  client_id: "test-client"
  client_secret: "test-secret"
`+tc.val)
			_, err := Load(path)
			require.Error(t, err)
			require.ErrorContains(t, err, "discovery_timeout")
		})
	}
}

func TestDiscoveryTimeoutPositiveOK(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  client_id: "test-client"
  client_secret: "test-secret"
  discovery_timeout: 5s
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, cfg.OIDC.DiscoveryTimeout)
}

func TestMaxRequestBytesPositiveOK(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {client_id: "test-client", client_secret: "test-secret"}
`,
			wantSub: "oidc.issuer is required",
		},
		{
			name: "missing oidc.client_id",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_secret: "test-secret"}
`,
			wantSub: "oidc.client_id is required",
		},
		{
			name: "missing oidc.client_secret",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client"}
`,
			wantSub: "oidc.client_secret is required",
		},
		{
			// A realistic pre-authorization-server config: issuer + audience and NO
			// client_id/client_secret (those didn't exist in the old resource-server schema).
			// The migration guard must fire here — not the generic "client_id is required"
			// error — so the operator gets the actionable pointer to config.example.yaml.
			name: "stale oidc.audience on old-schema config",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", audience: "mcp-docstore"}
`,
			wantSub: "oidc.audience is no longer used: the server is now its own OAuth issuer; see config.example.yaml",
		},
		{
			name: "missing public_url",
			yaml: `
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`,
			wantSub: "public_url is required",
		},
		{
			name: "missing bleve_index_path",
			yaml: `
public_url: "https://docs.example.com"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`,
			wantSub: "bleve_index_path is required",
		},
		{
			name: "empty tenant key",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
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
		{
			name: "oauth.registration invalid value",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth: {registration: "sometimes"}
`,
			wantSub: "oauth.registration must be one of open|allowlist",
		},
		{
			name: "oauth.registration allowlist with empty allowlist",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth: {registration: "allowlist"}
`,
			wantSub: "oauth.registration_allowlist must have at least one entry",
		},
		{
			name: "oauth.registration allowlist with non-https entry",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth:
  registration: "allowlist"
  registration_allowlist: ["http://client.example.com/callback"]
`,
			wantSub: "must be an https:// URL",
		},
		{
			name: "oauth.trusted_proxy_count negative",
			yaml: `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth: {trusted_proxy_count: -1}
`,
			wantSub: "oauth.trusted_proxy_count must be >= 0",
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

func TestOAuthRegistrationAllowlistAcceptsHTTPSEntries(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth:
  registration: "allowlist"
  registration_allowlist: ["https://client-a.example.com/callback", "https://client-b.example.com/callback"]
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "allowlist", cfg.OAuth.Registration)
	require.Equal(t, []string{"https://client-a.example.com/callback", "https://client-b.example.com/callback"}, cfg.OAuth.RegistrationAllowlist)
}

func TestOAuthCustomValues(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth:
  access_token_ttl: 30m
  refresh_token_ttl: 24h
  trust_proxy: true
  trusted_proxy_count: 2
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, cfg.OAuth.AccessTokenTTL)
	require.Equal(t, 24*time.Hour, cfg.OAuth.RefreshTokenTTL)
	require.True(t, cfg.OAuth.TrustProxy)
	require.Equal(t, 2, cfg.OAuth.TrustedProxyCount)
}

func TestOIDCRootCALoadsPool(t *testing.T) {
	certPath := writeTestCACertPEM(t)

	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  client_id: "test-client"
  client_secret: "test-secret"
  root_ca: "`+certPath+`"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.RootCAPool)
}

func TestOIDCRootCAMissingFileFailsLoad(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  client_id: "test-client"
  client_secret: "test-secret"
  root_ca: "/nonexistent/path/ca.pem"
`)
	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "root_ca")
}

func TestOIDCRootCABadPEMFailsLoad(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.pem")
	require.NoError(t, os.WriteFile(badPath, []byte("not a pem file"), 0o600))

	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc:
  issuer: "https://idp.example.com"
  client_id: "test-client"
  client_secret: "test-secret"
  root_ca: "`+badPath+`"
`)
	_, err := Load(path)
	require.Error(t, err)
	require.ErrorContains(t, err, "root_ca")
}

func TestOIDCAllowPrivateIPDefaultsFalse(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.OIDC.AllowPrivateIP)
}

func TestOIDCAllowPrivateIPTrue(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret", allow_private_ip: true}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.True(t, cfg.OIDC.AllowPrivateIP)
}

func TestLoggingDefaults(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "info", cfg.Logging.Level)
	require.Equal(t, "json", cfg.Logging.Format)
	require.Equal(t, "", cfg.Logging.ClientIPHeader)
}

func TestLoggingParsed(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
logging:
  level: debug
  format: text
  client_ip_header: "X-Forwarded-For"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "debug", cfg.Logging.Level)
	require.Equal(t, "text", cfg.Logging.Format)
	require.Equal(t, "X-Forwarded-For", cfg.Logging.ClientIPHeader)
}

func TestLoggingInvalid(t *testing.T) {
	base := `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`
	_, err := Load(writeTemp(t, base+"logging: {level: loud}\n"))
	require.ErrorContains(t, err, "logging.level")
	_, err = Load(writeTemp(t, base+"logging: {format: xml}\n"))
	require.ErrorContains(t, err, "logging.format")
}

// TestWebDisabledWhenAbsent asserts that with no web: block at all, the web UI defaults to
// disabled (the zero value of the now-value-typed WebConfig).
func TestWebDisabledWhenAbsent(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.Web.Enabled)
}

// TestWebEnabledTrue asserts web.enabled: true toggles the web UI on.
func TestWebEnabledTrue(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
web:
  enabled: true
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.True(t, cfg.Web.Enabled)
}

// TestWebEnabledFalse asserts an explicit web.enabled: false keeps the web UI disabled.
func TestWebEnabledFalse(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
web:
  enabled: false
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.Web.Enabled)
}

// TestWebRemovedKeysAreIgnored asserts stray removed keys (client_id, client_secret,
// redirect_url, post_logout_redirect_url, scopes, idle_timeout, absolute_timeout,
// sweep_interval, cookie_secure) in a web: block are simply ignored by viper's decode rather
// than causing a load failure, and that WebConfig no longer exposes those fields at all (a
// compile-time guarantee: this test wouldn't compile if the fields still existed and this
// file referenced them).
func TestWebRemovedKeysAreIgnored(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
web:
  enabled: true
  client_id: "stale-client-id"
  client_secret: "stale-secret"
  redirect_url: "https://docs.example.com/auth/callback"
  post_logout_redirect_url: "https://example.com"
  scopes: ["openid", "email"]
  idle_timeout: 24h
  absolute_timeout: 168h
  sweep_interval: 30m
  cookie_secure: false
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.True(t, cfg.Web.Enabled)
}

// TestOAuthCookieSecureDefaultsTrue asserts oauth.cookie_secure defaults to true (a pointer,
// so absence is distinguishable from an explicit false) when the oauth: block doesn't set it.
func TestOAuthCookieSecureDefaultsTrue(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.OAuth.CookieSecure)
	require.True(t, *cfg.OAuth.CookieSecure)
}

func TestOAuthCookieSecureExplicitFalse(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth:
  cookie_secure: false
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.OAuth.CookieSecure)
	require.False(t, *cfg.OAuth.CookieSecure)
}

// TestOAuthSweepIntervalDefault asserts oauth.sweep_interval defaults to 1h when unset.
func TestOAuthSweepIntervalDefault(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, time.Hour, cfg.OAuth.SweepInterval)
}

func TestOAuthSweepIntervalCustom(t *testing.T) {
	path := writeTemp(t, `
public_url: "https://docs.example.com"
bleve_index_path: "/tmp/idx.bleve"
database: {driver: sqlite, dsn: "x"}
oidc: {issuer: "https://idp.example.com", client_id: "test-client", client_secret: "test-secret"}
oauth:
  sweep_interval: 30m
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, cfg.OAuth.SweepInterval)
}
