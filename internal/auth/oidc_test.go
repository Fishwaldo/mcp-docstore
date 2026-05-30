package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

func TestOIDCVerifierVerifiesAndExtractsClaims(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "mcp-docstore", "email", "groups")
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
	v, err := NewOIDCVerifier(ctx, issuer, "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"some-other-service","sub":"s","exp":` + exp + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRejectsExpired(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	past := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s","exp":` + past + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRejectsWrongSigningKey(t *testing.T) {
	ctx := context.Background()
	issuer, _ := startOIDC(t)
	v, err := NewOIDCVerifier(ctx, issuer, "mcp-docstore", "email", "groups")
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
	v, err := NewOIDCVerifier(ctx, issuer, "mcp-docstore", "email", "groups")
	require.NoError(t, err)

	// Validly signed + correct aud + not expired, but claims a different issuer.
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"https://evil.example.com","aud":"mcp-docstore","sub":"s","exp":` + exp + `}`)
	_, err = v.Verify(ctx, tok)
	require.Error(t, err)
}

func TestOIDCVerifierRequiresAudience(t *testing.T) {
	issuer, _ := startOIDC(t)
	_, err := NewOIDCVerifier(context.Background(), issuer, "", "email", "groups")
	require.Error(t, err)
}
