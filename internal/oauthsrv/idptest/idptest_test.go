// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package idptest

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

const testClientID = "test-client"

// newTrustingClient returns an http.Client that trusts idp's self-signed certificate and does
// not follow redirects, so the caller can inspect /authorize's 302 Location header directly.
func newTrustingClient(idp *Server) *http.Client {
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: idp.RootCAs}},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// pkcePair generates a random RFC 7636 code_verifier and its S256 code_challenge.
func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// runAuthorize drives GET /authorize and returns the issued code and state parroted back.
func runAuthorize(t *testing.T, client *http.Client, idp *Server, challenge string) (code, state string) {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {testClientID},
		"redirect_uri":          {"https://client.example.com/callback"},
		"state":                 {"state-xyz"},
		"nonce":                 {"nonce-abc"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"openid email profile groups"},
	}
	resp, err := client.Get(idp.URL + "/authorize?" + q.Encode())
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "state-xyz", loc.Query().Get("state"))

	code = loc.Query().Get("code")
	require.NotEmpty(t, code)
	return code, loc.Query().Get("state")
}

func TestFullCodeFlow(t *testing.T) {
	idp := New(t)
	client := newTrustingClient(idp)

	verifier, challenge := pkcePair(t)
	code, _ := runAuthorize(t, client, idp, challenge)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://client.example.com/callback"},
		"client_id":     {testClientID},
		"client_secret": {"anything"},
		"code_verifier": {verifier},
	}
	resp, err := client.PostForm(idp.URL+"/token", form)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var tok tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	require.NotEmpty(t, tok.AccessToken)
	require.NotEmpty(t, tok.RefreshToken)
	require.Equal(t, "Bearer", tok.TokenType)
	require.Equal(t, 3600, tok.ExpiresIn)
	require.NotEmpty(t, tok.IDToken)

	// Verify the id_token's signature against /jwks and check its claims.
	jwksResp, err := client.Get(idp.URL + "/jwks")
	require.NoError(t, err)
	defer jwksResp.Body.Close()
	var jwks jose.JSONWebKeySet
	require.NoError(t, json.NewDecoder(jwksResp.Body).Decode(&jwks))
	require.Len(t, jwks.Keys, 1)

	parsed, err := jwt.ParseSigned(tok.IDToken, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)

	var claims map[string]any
	require.NoError(t, parsed.Claims(jwks.Keys[0].Key, &claims))
	require.Equal(t, idp.User.Sub, claims["sub"])
	require.Equal(t, idp.User.Email, claims["email"])
	require.Equal(t, testClientID, claims["aud"])
	require.Equal(t, idp.URL, claims["iss"])
	require.Equal(t, "nonce-abc", claims["nonce"])

	// Userinfo returns the currently configured user, including groups.
	req, err := http.NewRequest(http.MethodGet, idp.URL+"/userinfo", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uiResp, err := client.Do(req)
	require.NoError(t, err)
	defer uiResp.Body.Close()
	require.Equal(t, http.StatusOK, uiResp.StatusCode)

	var ui map[string]any
	require.NoError(t, json.NewDecoder(uiResp.Body).Decode(&ui))
	require.Equal(t, idp.User.Sub, ui["sub"])
	require.Equal(t, idp.User.Email, ui["email"])
	require.Equal(t, true, ui["email_verified"])
	groups, ok := ui["groups"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"engineering"}, groups)
}

func TestToken_PKCEMismatch(t *testing.T) {
	idp := New(t)
	client := newTrustingClient(idp)

	_, challenge := pkcePair(t)
	code, _ := runAuthorize(t, client, idp, challenge)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://client.example.com/callback"},
		"client_id":     {testClientID},
		"client_secret": {"anything"},
		"code_verifier": {"wrong-verifier-does-not-match-challenge"},
	}
	resp, err := client.PostForm(idp.URL+"/token", form)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDiscoveryDocument(t *testing.T) {
	idp := New(t)
	client := newTrustingClient(idp)

	resp, err := client.Get(idp.URL + "/.well-known/openid-configuration")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var doc discoveryDoc
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
	require.Equal(t, idp.URL, doc.Issuer)
	require.Equal(t, idp.URL+"/authorize", doc.AuthorizationEndpoint)
	require.Equal(t, idp.URL+"/token", doc.TokenEndpoint)
	require.Equal(t, idp.URL+"/userinfo", doc.UserinfoEndpoint)
	require.Equal(t, idp.URL+"/jwks", doc.JWKSURI)
	require.Equal(t, []string{"S256"}, doc.CodeChallengeMethodsSupported)
	require.Equal(t, []string{"public"}, doc.SubjectTypesSupported)
	require.Equal(t, []string{"RS256"}, doc.IDTokenSigningAlgValues)
	require.Contains(t, doc.ScopesSupported, "openid")
}
