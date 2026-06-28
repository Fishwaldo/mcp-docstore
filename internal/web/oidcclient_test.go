// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/stretchr/testify/require"
)

// startTestOIDC spins up an in-process OIDC server and returns its issuer URL.
func startTestOIDC(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: priv.Public(), KeyID: "test-key", Algorithm: oidc.RS256}},
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	srv.SetIssuer(ts.URL)
	return ts.URL
}

func TestOIDCClientAuthCodeURL(t *testing.T) {
	issuer := startTestOIDC(t)
	cfg := Config{
		ClientID:     "web-client",
		ClientSecret: "secret",
		RedirectURL:  "https://app.example/auth/callback",
		Scopes:       []string{"openid", "email", "profile", "groups"},
	}
	c, err := NewOIDCClient(context.Background(), issuer, cfg, "groups")
	require.NoError(t, err)

	raw := c.AuthCodeURL("state-123", "verifier-xyz")
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	require.Equal(t, "web-client", q.Get("client_id"))
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "state-123", q.Get("state"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("code_challenge"))
	require.True(t, strings.Contains(q.Get("scope"), "openid"))
}
