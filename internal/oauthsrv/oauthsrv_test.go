// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/giantswarm/mcp-oauth/security"
	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/entstore"
)

// recordingHandler is a minimal slog.Handler that captures every record it receives, so tests
// can assert on log content without parsing formatted text output.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// hasMessageContaining reports whether any captured record at the given level contains substr
// in its message.
func (h *recordingHandler) hasMessageContaining(level slog.Level, substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == level && strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}

// generateLocalhostCert creates a fresh self-signed certificate covering DNS name "localhost"
// and IP 127.0.0.1, so a test TLS server presenting it can be reached — and its hostname
// verified — via "https://localhost:PORT" rather than the loopback IP literal that
// dex.NewProvider's SSRF guard rejects as an issuer URL regardless of AllowPrivateIP (Go's stock
// httptest/testcert only covers 127.0.0.1/::1/example.com, none of which pass that guard).
func generateLocalhostCert(t *testing.T) (tls.Certificate, *x509.Certificate) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, cert
}

// startUpstreamOIDC spins up an in-process OIDC discovery server reachable over HTTPS at
// "https://localhost:PORT". Returns the issuer URL and a CertPool trusting the test server's
// self-signed certificate, for use as dex.Config.RootCAs (only honored when AllowPrivateIP is
// true — see providers/dex.resolveHTTPClient).
func startUpstreamOIDC(t *testing.T) (issuerURL string, rootCAs *x509.CertPool) {
	t.Helper()

	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	oidcSrv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: signingKey.Public(), KeyID: "upstream-key", Algorithm: oidc.RS256}},
	}

	tlsCert, leaf := generateLocalhostCert(t)
	ts := httptest.NewUnstartedServer(oidcSrv)
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	issuerURL = "https://localhost:" + u.Port()
	oidcSrv.SetIssuer(issuerURL)

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	return issuerURL, pool
}

func baseConfig(issuerURL string, rootCAs *x509.CertPool, allowPrivateIP bool) Config {
	return Config{
		PublicURL:            "https://docstore.example.com",
		UpstreamIssuer:       issuerURL,
		UpstreamClientID:     "test-client",
		UpstreamClientSecret: "test-secret",
		UpstreamScopes:       []string{"openid", "profile", "email", "groups"},
		AllowPrivateIP:       allowPrivateIP,
		RootCAs:              rootCAs,
		DiscoveryTimeout:     5 * time.Second,
		AccessTokenTTL:       time.Hour,
		RefreshTokenTTL:      90 * 24 * time.Hour,
		RegistrationOpen:     false,
	}
}

func newTestCombinedStore(t *testing.T, entc *ent.Client) storage.Combined {
	t.Helper()
	enc, err := security.NewEncryptor([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	return entstore.New(entc, enc, 24*time.Hour)
}

func TestNew_SucceedsAndJWTModeActive(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)
	require.NotNil(t, svc)

	keys, err := svc.PublicKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)

	pub, ok := keys[0].(*ecdsa.PublicKey)
	require.True(t, ok)
	require.True(t, pub.Equal(km.Signer.Public()))
}

func TestSeedWebClient_CreatesPublicClient(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)
	defer svc.Close()

	id, err := svc.SeedWebClient(context.Background())
	require.NoError(t, err)
	require.Equal(t, "docstore-web", id)

	client, err := svc.srv.GetClient(context.Background(), "docstore-web")
	require.NoError(t, err)
	require.Equal(t, storage.ClientTypePublic, client.ClientType)
	require.Empty(t, client.ClientSecretHash)
	require.Equal(t, "none", client.TokenEndpointAuthMethod)
	require.Equal(t, []string{"https://docstore.example.com/auth/callback"}, client.RedirectURIs)
	require.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, client.GrantTypes)
	require.Equal(t, []string{"code"}, client.ResponseTypes)
}

func TestSeedWebClient_SecondCallDoesNotRewrite(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)
	defer svc.Close()

	_, err = svc.SeedWebClient(context.Background())
	require.NoError(t, err)

	first, err := svc.srv.GetClient(context.Background(), "docstore-web")
	require.NoError(t, err)

	_, err = svc.SeedWebClient(context.Background())
	require.NoError(t, err)

	second, err := svc.srv.GetClient(context.Background(), "docstore-web")
	require.NoError(t, err)

	require.Equal(t, first.UpdatedAt, second.UpdatedAt, "second SeedWebClient call must not rewrite an already-matching client row")
}

func TestSeedWebClient_MismatchedShapeIsReseeded(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)
	defer svc.Close()

	// Simulate a pre-existing confidential BFF client row from before this migration: the
	// shape doesn't match (confidential, non-empty auth method), so SeedWebClient must
	// overwrite it rather than leaving the stale confidential record in place.
	require.NoError(t, svc.srv.SaveClient(context.Background(), &storage.Client{
		ClientID:                "docstore-web",
		ClientType:              storage.ClientTypeConfidential,
		ClientSecretHash:        "$2a$10$abcdefghijklmnopqrstuv",
		TokenEndpointAuthMethod: "client_secret_post",
		RedirectURIs:            []string{"https://docstore.example.com/auth/callback"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "DocStore Web UI",
	}))

	id, err := svc.SeedWebClient(context.Background())
	require.NoError(t, err)
	require.Equal(t, "docstore-web", id)

	client, err := svc.srv.GetClient(context.Background(), "docstore-web")
	require.NoError(t, err)
	require.Equal(t, storage.ClientTypePublic, client.ClientType)
	require.Empty(t, client.ClientSecretHash)
	require.Equal(t, "none", client.TokenEndpointAuthMethod)
}

func TestRateLimiters_WiredAndStoppedByClose(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)

	require.NotNil(t, svc.rateLimiter, "New must retain the per-IP rate limiter it wires into server.Config")
	require.NotNil(t, svc.clientRegRateLimiter, "New must retain the client-registration rate limiter it wires into server.Config")

	// Close must be safe to call and must stop both limiters' background cleanup goroutines.
	// There is no exported "is stopped" query, so this only asserts Close does not panic and
	// is idempotent-safe to call once; TestClose_SafeToCallOnce below covers double-Close.
	svc.Close()
}

func TestClose_SafeToCallTwice(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	logger := slog.New(slog.DiscardHandler)
	cfg := baseConfig(issuerURL, rootCAs, true)

	svc, err := New(context.Background(), cfg, st, km, entc, logger)
	require.NoError(t, err)

	svc.Close()
	require.NotPanics(t, svc.Close)
}

func TestNew_AllowPrivateIPTogglesWarnLog(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)

	t.Run("true logs the relaxed-SSRF warning", func(t *testing.T) {
		st := newTestCombinedStore(t, entc)
		rec := &recordingHandler{}
		logger := slog.New(rec)
		cfg := baseConfig(issuerURL, rootCAs, true)

		_, err := New(context.Background(), cfg, st, km, entc, logger)
		require.NoError(t, err)
		require.True(t, rec.hasMessageContaining(slog.LevelWarn, "SSRF protection relaxed"))
	})

	t.Run("false does not log the warning", func(t *testing.T) {
		st := newTestCombinedStore(t, entc)
		rec := &recordingHandler{}
		logger := slog.New(rec)
		// RootCAs is ignored on this path (only honored when AllowPrivateIP is true), so the
		// self-signed test certificate is not trusted by the system pool and construction is
		// expected to fail on the TLS handshake — the point of this sub-test is the absence of
		// the warning, not a successful build.
		cfg := baseConfig(issuerURL, nil, false)

		_, _ = New(context.Background(), cfg, st, km, entc, logger)
		require.False(t, rec.hasMessageContaining(slog.LevelWarn, "SSRF protection relaxed"))
	})
}

func TestNew_AllowPrivateIPRedirectsTogglesWarnLog(t *testing.T) {
	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)

	t.Run("true logs the redirect-URI relaxed warning", func(t *testing.T) {
		st := newTestCombinedStore(t, entc)
		rec := &recordingHandler{}
		logger := slog.New(rec)
		cfg := baseConfig(issuerURL, rootCAs, true) // AllowPrivateIP true so RootCAs is honored and build succeeds
		cfg.AllowPrivateIPRedirects = true

		_, err := New(context.Background(), cfg, st, km, entc, logger)
		require.NoError(t, err)
		require.True(t, rec.hasMessageContaining(slog.LevelWarn, "redirect URI SSRF protection relaxed"))
	})

	t.Run("false does not log the redirect-URI warning", func(t *testing.T) {
		st := newTestCombinedStore(t, entc)
		rec := &recordingHandler{}
		logger := slog.New(rec)
		cfg := baseConfig(issuerURL, rootCAs, true)
		cfg.AllowPrivateIPRedirects = false

		_, err := New(context.Background(), cfg, st, km, entc, logger)
		require.NoError(t, err)
		require.False(t, rec.hasMessageContaining(slog.LevelWarn, "redirect URI SSRF protection relaxed"))
	})
}
