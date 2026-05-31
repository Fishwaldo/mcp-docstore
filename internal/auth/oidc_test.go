// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/stretchr/testify/require"
)

// startOIDC spins up an in-process OIDC server and returns its issuer URL + signer.
func startOIDC(t *testing.T) (issuer string, sign func(claims string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: priv.Public(), KeyID: "test-key", Algorithm: oidc.RS256}},
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	srv.SetIssuer(ts.URL)
	return ts.URL, func(claims string) string {
		return oidctest.SignIDToken(priv, "test-key", oidc.RS256, claims)
	}
}

// startOIDCAtPath spins up an OIDC server whose metadata document is published at an
// off-spec metadataPath (in addition to the standard discovery location), mimicking a
// provider that serves RFC 8414 authorization-server metadata rather than OIDC
// discovery. It returns the issuer URL, the full metadata URL, and a token signer.
func startOIDCAtPath(t *testing.T, metadataPath string) (issuer, metadataURL string, sign func(claims string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: priv.Public(), KeyID: "test-key", Algorithm: oidc.RS256}},
	}
	var base string
	mux := http.NewServeMux()
	mux.Handle("/", srv)
	// Re-serve the standard discovery document verbatim at the off-spec path.
	mux.HandleFunc(metadataPath, func(w http.ResponseWriter, _ *http.Request) {
		resp, err := http.Get(base + "/.well-known/openid-configuration")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, resp.Body)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	base = ts.URL
	srv.SetIssuer(ts.URL)
	return ts.URL, ts.URL + metadataPath, func(claims string) string {
		return oidctest.SignIDToken(priv, "test-key", oidc.RS256, claims)
	}
}

func TestOIDCVerifierDiscoveryURL(t *testing.T) {
	ctx := context.Background()
	issuer, metadataURL, sign := startOIDCAtPath(t, "/.well-known/oauth-authorization-server")
	v, err := NewOIDCVerifier(ctx, issuer, metadataURL, "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"sub-1","exp":` + exp +
		`,"email":"alice@acme.com","groups":["eng"]}`)

	claims, err := v.Verify(ctx, tok)
	require.NoError(t, err)
	require.Equal(t, "sub-1", claims.Subject)
	require.Equal(t, "alice@acme.com", claims.Email)
	require.Equal(t, []string{"eng"}, claims.Groups)
}

func TestOIDCVerifierDiscoveryURLRejectsIssuerMismatch(t *testing.T) {
	ctx := context.Background()
	_, metadataURL, _ := startOIDCAtPath(t, "/.well-known/oauth-authorization-server")
	// The metadata document's issuer is the server URL; a different configured issuer
	// must fail construction rather than silently trusting the document's keys.
	_, err := NewOIDCVerifier(ctx, "https://wrong.example.com", metadataURL, "mcp-docstore", "email", "groups")
	require.Error(t, err)
}

func TestOIDCVerifierDiscoveryURLRejectsMissingDocument(t *testing.T) {
	ctx := context.Background()
	issuer, _, _ := startOIDCAtPath(t, "/.well-known/oauth-authorization-server")
	_, err := NewOIDCVerifier(ctx, issuer, issuer+"/.well-known/does-not-exist", "mcp-docstore", "email", "groups")
	require.Error(t, err)
}

func TestOIDCVerifierVerifiesAndExtractsClaims(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"sub-1","exp":` + exp +
		`,"email":"alice@acme.com","groups":["eng","ops"]}`)

	claims, err := v.Verify(ctx, tok)
	require.NoError(t, err)
	require.Equal(t, "sub-1", claims.Subject)
	require.Equal(t, "alice@acme.com", claims.Email)
	require.Equal(t, []string{"eng", "ops"}, claims.Groups)
	require.False(t, claims.Expiry.IsZero())
	require.WithinDuration(t, time.Now().Add(time.Hour), claims.Expiry, 2*time.Minute)
}

func TestOIDCVerifierRejectsWrongAudience(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"some-other-service","sub":"s","exp":` + exp + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRejectsExpired(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	past := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s","exp":` + past + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRejectsWrongSigningKey(t *testing.T) {
	ctx := context.Background()
	issuer, _ := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	// Sign with a key the server does NOT publish → signature must not verify.
	rogue, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := oidctest.SignIDToken(rogue, "test-key", oidc.RS256,
		`{"iss":"`+issuer+`","aud":"mcp-docstore","sub":"s","exp":`+exp+`}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRejectsWrongIssuer(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	// Validly signed + correct aud + not expired, but claims a different issuer.
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"https://evil.example.com","aud":"mcp-docstore","sub":"s","exp":` + exp + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRequiresAudience(t *testing.T) {
	issuer, _ := startOIDC(t)
	_, err := NewOIDCVerifier(context.Background(), issuer, "", "", "email", "groups")
	require.Error(t, err)
}
