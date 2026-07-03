// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server_test

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/idptest"
)

// This file is the black-box conformance suite for the embedded OAuth authorization server:
// it drives the real assembled stack (cmd/server.Run) exactly as a third-party MCP client
// would — dynamic client registration, the human consent gate, a PKCE authorization-code
// flow federated through a fake upstream IdP, token exchange, refresh rotation, revocation,
// and the discovery documents — with no mocked AS and no stubbed verifier. Every helper here
// operates purely over HTTP against a booted cmd/server.Run instance (see server_test.go's
// baseConfig/freeAddr/waitReady/runServer, reused unmodified).
//
// conformanceRedirectURI is an HTTPS public-IP-literal redirect URI used throughout this file
// in place of the brief's illustrative "http://127.0.0.1:PORT/cb": the assembled server's
// oauthsrv.Config (internal/oauthsrv/oauthsrv.go) never sets server.Config.AllowLocalhostRedirectURIs,
// which Go zero-values to false, so an http:// loopback redirect_uri is rejected at
// registration (RFC 8252 native-app support is simply not wired up yet — a real product gap,
// but outside this test-writing task's scope to fix). An IP literal avoids that gate and also
// avoids a real DNS lookup (mcp-oauth's DNS validation defaults strict-on), matching the
// pattern internal/oauthsrv/mount_test.go already uses for the same reason.
const conformanceRedirectURIHost = "93.184.216.34"

// registerClient performs POST /oauth/register for a client named name with a single
// redirect_uris entry, deliberately omitting token_endpoint_auth_method and client_type so
// the library's default resolution (RFC 7591 §2) applies: this yields a confidential client
// with a generated client_secret, matching real-world DCR clients (e.g. claude.ai) that
// register without declaring an auth method. It returns the decoded JSON response alongside
// the client_id/client_secret for convenience.
func registerClient(t *testing.T, addr, redirectURI, name string) (clientID, clientSecret string, resp map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"client_name":   name,
		"redirect_uris": []string{redirectURI},
	})
	require.NoError(t, err)

	httpResp, err := http.Post("http://"+addr+"/oauth/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, httpResp.StatusCode, "register response: %s", respBody)

	var m map[string]any
	require.NoError(t, json.Unmarshal(respBody, &m))
	id, _ := m["client_id"].(string)
	secret, _ := m["client_secret"].(string)
	require.NotEmpty(t, id)
	return id, secret, m
}

// consentCSRFFieldRE extracts the CSRF token rendered into the consent page's hidden form
// field; mirrors internal/oauthsrv/mount_test.go's csrfFieldRE (a private, unexported var in a
// different package, so it is not reusable here).
var consentCSRFFieldRE = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

// publicHostRedirectTransport reroutes any request addressed to publicHost (the configured
// public_url's hostname — a placeholder like "docs.example.com" that resolves nowhere in a
// test environment) to the real listener address instead, rewriting scheme to plain HTTP to
// match how cmd/server.Run actually serves in these tests. This stands in for the reverse
// proxy / DNS a real deployment would have in front of the AS: the upstream IdP's login
// redirect legitimately targets the AS's own {public_url}/oauth/callback, and this suite needs
// that hop to land on the real in-process listener rather than fail DNS resolution. Every
// other request (direct calls to the real addr, and the upstream idptest calls) passes
// through untouched.
type publicHostRedirectTransport struct {
	base       http.RoundTripper
	publicHost string
	realAddr   string
}

func (t *publicHostRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() != t.publicHost {
		return t.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.realAddr
	req.Host = t.realAddr
	return t.base.RoundTrip(req)
}

// newBrowser builds an http.Client with a cookie jar (so the consent and TLS-trusted upstream
// idptest hops share cookies/session state the way a real browser would) whose CheckRedirect
// stops the instant a redirect targets stopHost — the registered client's own redirect_uri,
// which this suite never actually stands up a server for. Every earlier hop (the consent
// POST's 303, the AS's redirect to the upstream IdP, idptest's auto-login redirect back to our
// AS's own {publicHost}/oauth/callback, rerouted to realAddr by publicHostRedirectTransport) is
// followed automatically, so the *http.Response returned by the final call is the AS's 3xx
// redirect to the client's redirect_uri, Location header intact.
func newBrowser(t *testing.T, upstreamCAs *tls.Config, stopHost, publicHost, realAddr string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &http.Client{
		Jar: jar,
		Transport: &publicHostRedirectTransport{
			base:       &http.Transport{TLSClientConfig: upstreamCAs},
			publicHost: publicHost,
			realAddr:   realAddr,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Hostname() == stopHost {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			return nil
		},
	}
}

// obtainAuthCode drives GET /oauth/authorize?query through the human consent gate (this
// suite's client_ids are all dynamically registered third parties, so every one hits the
// consent page — only the first-party BFF client_id bypasses it) and the upstream idptest
// login, returning the authorization code and state captured off the final redirect to the
// client's own redirect_uri. client's cookie jar carries both the ds_oauth_csrf cookie (set
// when the consent page is rendered) and the ds_oauth_consent cookie (set once approved)
// automatically; net/http's Client applies Jar cookies to every request in the chain, so no
// cookie is threaded through by hand here.
func obtainAuthCode(t *testing.T, client *http.Client, addr string, query url.Values) (code, state string) {
	t.Helper()

	resp, err := client.Get("http://" + addr + "/oauth/authorize?" + query.Encode())
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	if resp.StatusCode == http.StatusOK && strings.Contains(string(body), `action="/oauth/consent"`) {
		m := consentCSRFFieldRE.FindStringSubmatch(string(body))
		require.Len(t, m, 2, "consent page must render a CSRF token; body=%s", body)

		form := url.Values{
			"authorize_query": {query.Encode()},
			"csrf":            {m[1]},
			"decision":        {"approve"},
		}
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/oauth/consent", strings.NewReader(form.Encode()))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err = client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
	}

	require.Equal(t, http.StatusFound, resp.StatusCode, "expected the final redirect back to the client's redirect_uri")
	loc := resp.Header.Get("Location")
	require.NotEmpty(t, loc, "redirect must carry a Location header")
	u, err := url.Parse(loc)
	require.NoError(t, err)
	code = u.Query().Get("code")
	state = u.Query().Get("state")
	require.NotEmpty(t, code, "redirect Location must carry an authorization code: %s", loc)
	return code, state
}

// exchangeToken POSTs grant_type=authorization_code to /oauth/token, authenticating with HTTP
// Basic (the only client-authentication mechanism the library's token endpoint accepts for a
// confidential client, regardless of the registered token_endpoint_auth_method). resource, when
// empty, is omitted from the form entirely rather than sent as "".
func exchangeToken(t *testing.T, addr, clientID, clientSecret, code, redirectURI, verifier, resource string) (status int, resp map[string]any) {
	t.Helper()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	if resource != "" {
		form.Set("resource", resource)
	}
	return postToken(t, addr, clientID, clientSecret, form)
}

// refreshToken POSTs grant_type=refresh_token to /oauth/token, again authenticating with HTTP
// Basic since refresh grants for a confidential client require the same client authentication
// as the initial code exchange.
func refreshToken(t *testing.T, addr, clientID, clientSecret, token string) (status int, resp map[string]any) {
	t.Helper()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token},
	}
	return postToken(t, addr, clientID, clientSecret, form)
}

func postToken(t *testing.T, addr, clientID, clientSecret string, form url.Values) (status int, resp map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/oauth/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	httpResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer httpResp.Body.Close()
	body, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err)

	var m map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &m)
	}
	return httpResp.StatusCode, m
}

// revokeToken POSTs RFC 7009 /oauth/revoke for token, authenticated with the client's HTTP
// Basic credentials, and asserts the endpoint's mandated 200 (it always returns success
// regardless of whether the token was found, per RFC 7009).
func revokeToken(t *testing.T, addr, clientID, clientSecret, token string) {
	t.Helper()
	form := url.Values{"token": {token}, "client_id": {clientID}}
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/oauth/revoke", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// decodeJWTPayload base64url-decodes an RFC 9068 access token's middle (claims) segment
// without verifying its signature — this suite only ever inspects claims of tokens the server
// itself just minted (or, in the tampered-signature cross-check, deliberately checks that the
// *server* rejects it; this helper never substitutes for that check).
func decodeJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "expected a JWT access token, got: %s", token)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// tamperJWTSignature flips the second-to-last character of token's signature segment, so it
// is a syntactically well-formed JWT whose signature no longer verifies. It deliberately does
// NOT flip the very last character: base64url's final quantum for a 64-byte ES256 signature
// (64 = 21*3 + 1) trails with a partial 2-character group whose second character only
// contributes its top 2 bits to the decoded byte — the low 4 bits are unused padding RFC 4648
// does not require decoders to reject — so toggling 'A'<->'B' there (both share the same top-2
// bits, 0) silently decodes to the SAME bytes and the "tampered" token still verifies. The
// second-to-last character is always either inside a full 4-character/3-byte group or the
// first character of a trailing partial group, both of which are fully bit-significant, so
// changing it always changes the decoded signature bytes.
func tamperJWTSignature(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	b := []byte(parts[2])
	require.Greater(t, len(b), 1)
	idx := len(b) - 2
	if b[idx] == 'A' {
		b[idx] = 'B'
	} else {
		b[idx] = 'A'
	}
	parts[2] = string(b)
	return strings.Join(parts, ".")
}

// audMatches reports whether a JWT "aud" claim - decoded as either a bare string or a string
// array, both legal JSON shapes for that claim - contains want.
func audMatches(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// mcpInitialize POSTs a minimal MCP initialize request to /mcp with bearer as the Authorization
// header's token (an empty bearer omits the header entirely). The status code alone answers
// the only question this suite asks of /mcp: did the LocalVerifier accept or reject the token.
// A non-401 response proves acceptance regardless of whatever the MCP layer does next.
func mcpInitialize(t *testing.T, addr, bearer string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"conformance-test","version":"0.0.1"}}}`
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// authorizeQuery builds a base GET /oauth/authorize query for clientID/redirectURI with a
// fresh S256 PKCE pair, returning the query and the verifier the token exchange will need.
// scope/resource, when empty, are omitted entirely (scenario 2 exercises exactly this).
func authorizeQuery(clientID, redirectURI, state, scope, resource string) (query url.Values, verifier string) {
	verifier = oauth2.GenerateVerifier()
	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(verifier)},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	if resource != "" {
		q.Set("resource", resource)
	}
	return q, verifier
}

// TestOAuthConformance is the black-box conformance suite for the embedded authorization
// server: dynamic client registration, human consent, PKCE, token exchange (with and without
// optional RFC 8707 resource / scope), authorization-code and refresh-token reuse detection,
// revocation, and the discovery documents' shape — all driven over real HTTP against a single
// booted cmd/server.Run instance federating to an idptest upstream. Each subtest registers its
// own client so the scenarios don't interfere with one another's consent/token state.
func TestOAuthConformance(t *testing.T) {
	idp := idptest.New(t)
	// baseConfig's sole tenant matches emails at "acme.com"; the fake IdP's default user is
	// "user@example.com", which no tenant admits (store.UpsertUser rejects it as
	// "email_not_onboarded" before an identity is ever established). Rehome the fake user to
	// the tenant's domain so the flows below actually mint a usable identity.
	idp.User.Email = "user@acme.com"
	addr := freeAddr(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, addr, ""))
	stop := runServer(t, cfgPath)
	defer stop()
	waitReady(t, addr)

	upstreamTLS := &tls.Config{RootCAs: idp.RootCAs}
	const mcpResource = "https://docs.example.com/mcp" // {public_url}/mcp per baseConfig

	// Scenario 1: DCR -> consent -> PKCE -> MCP call. This is the Claude Code onboarding path,
	// so it deliberately registers a REAL RFC 8252 native-app loopback redirect (an ephemeral
	// http://127.0.0.x:PORT/... callback) rather than the HTTPS IP-literal the other scenarios
	// use. That exercises oauthsrv.New's AllowLocalhostRedirectURIs=true: without it, DCR would
	// reject this redirect at registration and native-app onboarding would be broken. The port
	// is fixed and nothing listens on it — obtainAuthCode captures the code off the AS's 302
	// Location before the browser would ever dial the callback. The host is 127.0.0.2 (still in
	// 127.0.0.0/8, so the library accepts it as loopback) purely so it stays distinct from the
	// two other loopback hosts already in play that the browser's redirect-stop must NOT fire
	// on: the real AS listener (127.0.0.1) and the upstream idptest (localhost).
	t.Run("DCR_Consent_PKCE_MCPCall", func(t *testing.T) {
		const loopbackHost = "127.0.0.2"
		redirectURI := "http://" + loopbackHost + ":49152/cb1"
		clientID, clientSecret, reg := registerClient(t, addr, redirectURI, "Conformance Client 1")

		// claude.ai Zod-parsing regression guard: optional URI fields must be ABSENT from the
		// JSON, never present-as-empty-string (Zod's z.string().url().optional() rejects "").
		_, hasClientURI := reg["client_uri"]
		require.False(t, hasClientURI, "client_uri must be absent from the registration response, not an empty string")
		require.Contains(t, reg, "client_secret_expires_at", "a confidential client's response must carry client_secret_expires_at")
		require.NotEmpty(t, clientSecret)

		query, verifier := authorizeQuery(clientID, redirectURI, "state-1-0123456789abcdef0123456789",
			"openid profile email groups offline_access", mcpResource)

		browser := newBrowser(t, upstreamTLS, loopbackHost, "docs.example.com", addr)
		code, gotState := obtainAuthCode(t, browser, addr, query)
		require.Equal(t, "state-1-0123456789abcdef0123456789", gotState)

		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, mcpResource)
		require.Equal(t, http.StatusOK, status, "token exchange failed: %v", tok)

		accessToken, _ := tok["access_token"].(string)
		refreshTok, _ := tok["refresh_token"].(string)
		require.NotEmpty(t, accessToken)
		require.NotEmpty(t, refreshTok)

		claims := decodeJWTPayload(t, accessToken)
		require.Equal(t, "https://docs.example.com", claims["iss"])
		require.True(t, audMatches(claims["aud"], mcpResource), "aud claim: %v", claims["aud"])
		require.Equal(t, idp.User.Email, claims["email"])
		groups, _ := claims["groups"].([]any)
		require.Len(t, groups, len(idp.User.Groups))
		for i, g := range idp.User.Groups {
			require.Equal(t, g, groups[i])
		}

		mcpStatus := mcpInitialize(t, addr, accessToken)
		require.NotEqual(t, http.StatusUnauthorized, mcpStatus, "verifier must accept a freshly minted access token")
	})

	// Scenario 2: authorize with no scope, token exchange with no resource; both still
	// succeed and the minted aud defaults to {public}/mcp.
	t.Run("MissingScopeAndResource", func(t *testing.T) {
		redirectURI := "https://" + conformanceRedirectURIHost + "/cb2"
		clientID, clientSecret, _ := registerClient(t, addr, redirectURI, "Conformance Client 2")

		query, verifier := authorizeQuery(clientID, redirectURI, "state-2-0123456789abcdef0123456789", "", "")
		require.NotContains(t, query, "scope")
		require.NotContains(t, query, "resource")

		browser := newBrowser(t, upstreamTLS, conformanceRedirectURIHost, "docs.example.com", addr)
		code, _ := obtainAuthCode(t, browser, addr, query)

		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusOK, status, "token exchange failed: %v", tok)

		accessToken, _ := tok["access_token"].(string)
		require.NotEmpty(t, accessToken)
		claims := decodeJWTPayload(t, accessToken)
		require.True(t, audMatches(claims["aud"], mcpResource), "aud must default to {public}/mcp, got: %v", claims["aud"])
	})

	// Scenario 3: replaying a spent authorization code is rejected, and the refresh token
	// minted alongside the original (now-reused) code is revoked too.
	t.Run("AuthCodeReuse", func(t *testing.T) {
		redirectURI := "https://" + conformanceRedirectURIHost + "/cb3"
		clientID, clientSecret, _ := registerClient(t, addr, redirectURI, "Conformance Client 3")

		query, verifier := authorizeQuery(clientID, redirectURI, "state-3-0123456789abcdef0123456789", "openid offline_access", "")
		browser := newBrowser(t, upstreamTLS, conformanceRedirectURIHost, "docs.example.com", addr)
		code, _ := obtainAuthCode(t, browser, addr, query)

		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusOK, status, "first exchange must succeed: %v", tok)
		refreshTok, _ := tok["refresh_token"].(string)
		require.NotEmpty(t, refreshTok)

		replayStatus, replayBody := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusBadRequest, replayStatus, "a replayed authorization code must be rejected: %v", replayBody)
		require.Equal(t, "invalid_grant", replayBody["error"], "reuse must be an OAuth invalid_grant, not an unrelated 400: %v", replayBody)

		refreshStatus, refreshBody := refreshToken(t, addr, clientID, clientSecret, refreshTok)
		require.Equal(t, http.StatusBadRequest, refreshStatus,
			"code reuse must revoke the refresh token family: %v", refreshBody)
		require.Equal(t, "invalid_grant", refreshBody["error"], "the revoked refresh must be an invalid_grant: %v", refreshBody)
	})

	// Scenario 4: refresh rotation mints a new access+refresh pair; reusing the old refresh
	// token errors AND revokes the family, so the new one dies too.
	t.Run("RefreshRotationAndReuseDetection", func(t *testing.T) {
		redirectURI := "https://" + conformanceRedirectURIHost + "/cb4"
		clientID, clientSecret, _ := registerClient(t, addr, redirectURI, "Conformance Client 4")

		query, verifier := authorizeQuery(clientID, redirectURI, "state-4-0123456789abcdef0123456789", "openid offline_access", "")
		browser := newBrowser(t, upstreamTLS, conformanceRedirectURIHost, "docs.example.com", addr)
		code, _ := obtainAuthCode(t, browser, addr, query)

		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusOK, status, "%v", tok)
		refreshTok1, _ := tok["refresh_token"].(string)
		require.NotEmpty(t, refreshTok1)

		rotateStatus, rotated := refreshToken(t, addr, clientID, clientSecret, refreshTok1)
		require.Equal(t, http.StatusOK, rotateStatus, "refresh must succeed: %v", rotated)
		accessTok2, _ := rotated["access_token"].(string)
		refreshTok2, _ := rotated["refresh_token"].(string)
		require.NotEmpty(t, accessTok2)
		require.NotEmpty(t, refreshTok2)
		require.NotEqual(t, refreshTok1, refreshTok2, "refresh must rotate to a new token")

		reuseStatus, reuseBody := refreshToken(t, addr, clientID, clientSecret, refreshTok1)
		require.Equal(t, http.StatusBadRequest, reuseStatus, "reusing the old refresh token must error: %v", reuseBody)
		require.Equal(t, "invalid_grant", reuseBody["error"], "refresh reuse must be an invalid_grant: %v", reuseBody)

		deadStatus, deadBody := refreshToken(t, addr, clientID, clientSecret, refreshTok2)
		require.Equal(t, http.StatusBadRequest, deadStatus,
			"the rotated-to refresh token must also be dead once its family is revoked: %v", deadBody)
		require.Equal(t, "invalid_grant", deadBody["error"], "the family-revoked refresh must be an invalid_grant: %v", deadBody)
	})

	// Scenario 5: revoking an access token via /oauth/revoke must make LocalVerifier reject
	// it on the very next /mcp call. This exercises the jti-keying cross-layer contract:
	// /oauth/revoke records the token's jti on the denylist, and LocalVerifier.Verify checks
	// that denylist, so a just-revoked token must fail verification without any token refresh.
	t.Run("Revocation", func(t *testing.T) {
		redirectURI := "https://" + conformanceRedirectURIHost + "/cb5"
		clientID, clientSecret, _ := registerClient(t, addr, redirectURI, "Conformance Client 5")

		query, verifier := authorizeQuery(clientID, redirectURI, "state-5-0123456789abcdef0123456789", "openid", "")
		browser := newBrowser(t, upstreamTLS, conformanceRedirectURIHost, "docs.example.com", addr)
		code, _ := obtainAuthCode(t, browser, addr, query)

		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusOK, status, "%v", tok)
		accessToken, _ := tok["access_token"].(string)
		require.NotEmpty(t, accessToken)

		require.NotEqual(t, http.StatusUnauthorized, mcpInitialize(t, addr, accessToken),
			"token must be valid before revocation")

		revokeToken(t, addr, clientID, clientSecret, accessToken)

		require.Equal(t, http.StatusUnauthorized, mcpInitialize(t, addr, accessToken),
			"a revoked access token must be rejected by LocalVerifier.IsJTIRevoked")
	})

	// Scenario 7: cross-checks not covered above.
	t.Run("CrossChecks", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, mcpInitialize(t, addr, "not-a-real-token"),
			"a garbage bearer token must be rejected")

		redirectURI := "https://" + conformanceRedirectURIHost + "/cb7"
		clientID, clientSecret, _ := registerClient(t, addr, redirectURI, "Conformance Client 7")
		query, verifier := authorizeQuery(clientID, redirectURI, "state-7-0123456789abcdef0123456789", "openid", "")
		browser := newBrowser(t, upstreamTLS, conformanceRedirectURIHost, "docs.example.com", addr)
		code, _ := obtainAuthCode(t, browser, addr, query)
		status, tok := exchangeToken(t, addr, clientID, clientSecret, code, redirectURI, verifier, "")
		require.Equal(t, http.StatusOK, status, "%v", tok)
		accessToken, _ := tok["access_token"].(string)
		require.NotEmpty(t, accessToken)

		tampered := tamperJWTSignature(t, accessToken)
		require.Equal(t, http.StatusUnauthorized, mcpInitialize(t, addr, tampered),
			"a token with a tampered signature must be rejected")

		metaResp, err := http.Get("http://" + addr + "/.well-known/openid-configuration")
		require.NoError(t, err)
		metaBody, err := io.ReadAll(metaResp.Body)
		require.NoError(t, err)
		require.NoError(t, metaResp.Body.Close())
		require.Equal(t, http.StatusOK, metaResp.StatusCode)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(metaBody, &meta))
		require.Contains(t, meta, "registration_endpoint")
		methods, _ := meta["code_challenge_methods_supported"].([]any)
		require.Contains(t, methods, "S256")
	})
}

// TestOAuthConformanceRegistrationAllowlist boots a second, independent server with
// oauth.registration: allowlist and asserts dynamic client registration is gated by an
// exact-match redirect URI allowlist: an arbitrary redirect is rejected, the allowlisted one
// succeeds.
func TestOAuthConformanceRegistrationAllowlist(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)

	// A claude.ai-style callback path, on an HTTPS public-IP-literal host so no real
	// (strict-by-default) DNS lookup is needed. The rejected case below reuses this SAME host
	// and differs only by path, so the allowlist's exact-match — not any hostname or
	// scheme difference — is provably the sole variable under test.
	allowlisted := "https://" + conformanceRedirectURIHost + "/api/mcp/auth_callback"
	notAllowlisted := "https://" + conformanceRedirectURIHost + "/some/other/callback"
	cfg := baseConfig(t, idp, addr, "") +
		"oauth:\n" +
		"  registration: allowlist\n" +
		"  registration_allowlist:\n" +
		"    - \"" + allowlisted + "\"\n"
	cfgPath := writeConfig(t, cfg)
	stop := runServer(t, cfgPath)
	defer stop()
	waitReady(t, addr)

	t.Run("NonAllowlistedRedirectRejected", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"client_name":   "Non-allowlisted Client",
			"redirect_uris": []string{notAllowlisted},
		})
		require.NoError(t, err)
		resp, err := http.Post("http://"+addr+"/oauth/register", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.GreaterOrEqual(t, resp.StatusCode, 400)
		require.Less(t, resp.StatusCode, 500)
	})

	t.Run("AllowlistedRedirectAccepted", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"client_name":   "Claude-style Client",
			"redirect_uris": []string{allowlisted},
		})
		require.NoError(t, err)
		resp, err := http.Post("http://"+addr+"/oauth/register", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode, "body=%s", respBody)
	})
}
