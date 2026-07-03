// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
)

// stubVerifier is a minimal accessTokenVerifier double: it returns fixed claims or a fixed
// error, without validating rawToken at all — the real verification behavior belongs to
// *auth.LocalVerifier and is exercised end-to-end in authflow_test.go.
type stubVerifier struct {
	claims *auth.Claims
	err    error
}

func (v stubVerifier) Verify(context.Context, string) (*auth.Claims, error) {
	if v.err != nil {
		return nil, v.err
	}
	return v.claims, nil
}

func TestASClientAuthCodeURL(t *testing.T) {
	c := NewASClient("https://issuer.example", "web-client", "secret", "https://app.example/auth/callback", stubVerifier{}, nil)

	raw := c.AuthCodeURL("state-123", "verifier-xyz")
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "issuer.example", u.Host)
	require.Equal(t, "/oauth/authorize", u.Path)

	q := u.Query()
	require.Equal(t, "web-client", q.Get("client_id"))
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "state-123", q.Get("state"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("code_challenge"))
	require.True(t, strings.Contains(q.Get("scope"), "openid"))
	require.True(t, strings.Contains(q.Get("scope"), "offline_access"))
}

// fakeTokenEndpoint stands up a minimal token endpoint so asClient.Exchange can be unit-tested
// in isolation: it accepts any authorization_code request and returns a fixed access token.
// It does not validate PKCE or client credentials — the full authorization-code + PKCE
// round trip against a real authorization server is exercised in authflow_test.go; this test
// only covers asClient's own exchange-then-verify wiring.
func fakeTokenEndpoint(t *testing.T, accessToken string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "rt-1",
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestASClientExchangeVerifiesAccessToken(t *testing.T) {
	ts := fakeTokenEndpoint(t, "at-123")
	claims := &auth.Claims{Subject: "s1", Email: "alice@acme.com"}
	c := NewASClient(ts.URL, "web-client", "secret", "https://app.example/auth/callback", stubVerifier{claims: claims}, nil)

	gotClaims, rawToken, tok, err := c.Exchange(context.Background(), "code-1", "verifier-1")
	require.NoError(t, err)
	require.Equal(t, claims, gotClaims)
	require.Equal(t, "at-123", rawToken)
	require.Equal(t, "at-123", tok.AccessToken)
}

func TestASClientExchangeRejectsVerifyFailure(t *testing.T) {
	ts := fakeTokenEndpoint(t, "at-123")
	c := NewASClient(ts.URL, "web-client", "secret", "https://app.example/auth/callback", stubVerifier{err: errors.New("verify failed")}, nil)

	_, _, _, err := c.Exchange(context.Background(), "code-1", "verifier-1")
	require.Error(t, err)
}

// muxTransport dispatches an HTTP request straight to an in-process handler via
// httptest.NewRecorder, so a caller's outbound HTTP call never leaves the process. It mirrors
// the production transport cmd/server builds to route the BFF's calls to its own embedded
// authorization server without depending on public_url being reachable from inside the
// container.
type muxTransport struct{ h http.Handler }

func (t muxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestASClientExchangeUsesInjectedTransport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-inproc",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	claims := &auth.Claims{Subject: "s1", Email: "alice@acme.com"}
	// "issuer.invalid" resolves nowhere on a real network; the exchange can only succeed via
	// the injected in-process transport, proving Exchange never dials out for this call.
	c := NewASClient("https://issuer.invalid", "web-client", "secret", "https://app.example/auth/callback",
		stubVerifier{claims: claims}, muxTransport{h: mux})

	gotClaims, rawToken, _, err := c.Exchange(context.Background(), "code-1", "verifier-1")
	require.NoError(t, err)
	require.Equal(t, claims, gotClaims)
	require.Equal(t, "at-inproc", rawToken)
}
