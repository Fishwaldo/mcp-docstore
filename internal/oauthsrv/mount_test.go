// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// csrfFieldRE extracts the CSRF token rendered into the consent page's hidden form field.
var csrfFieldRE = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

// getConsentPage performs the consent GET for query and returns the ds_oauth_csrf cookie the
// server set plus the CSRF token it rendered into the form — the exact pair a real browser
// would submit. It asserts the response is in fact the consent page.
func getConsentPage(t *testing.T, client *http.Client, baseURL, query string) (*http.Cookie, string) {
	t.Helper()
	resp, err := client.Get(baseURL + "/oauth/authorize?" + query)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), consentFormMarker)

	var csrfCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == csrfCookieName {
			csrfCookie = c
		}
	}
	require.NotNil(t, csrfCookie, "consent GET must set the ds_oauth_csrf cookie")

	m := csrfFieldRE.FindStringSubmatch(string(body))
	require.Len(t, m, 2, "consent page must render a CSRF token in the form")
	return csrfCookie, m[1]
}

// thirdPartyClientID/thirdPartyRedirectURI identify a dynamically-registered client used
// throughout these tests to stand in for "some third party that is not our first-party BFF".
const (
	thirdPartyClientID    = "third-party-app"
	thirdPartyClientName  = "Third Party App"
	thirdPartyRedirectURI = "https://third-party.example.com/callback"
)

// newMountTestService builds a fully assembled Service (upstream OIDC test server, ent-backed
// storage, key material) the same way oauthsrv_test.go's TestNew_SucceedsAndJWTModeActive
// does, then seeds the first-party BFF client and one third-party client for the consent-gate
// tests to exercise.
func newMountTestService(t *testing.T, registrationOpen bool) *Service {
	t.Helper()

	issuerURL, rootCAs := startUpstreamOIDC(t)
	entc := newTestEntClient(t)
	km, err := LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)
	st := newTestCombinedStore(t, entc)

	cfg := baseConfig(issuerURL, rootCAs, true)
	cfg.RegistrationOpen = registrationOpen
	cfg.CookieSecure = false // tests run over plain http.Client, not TLS

	svc, err := New(context.Background(), cfg, st, km, entc, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(svc.Close)

	_, err = svc.SeedWebClient(context.Background())
	require.NoError(t, err)

	require.NoError(t, svc.srv.SaveClient(context.Background(), &storage.Client{
		ClientID:                thirdPartyClientID,
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{thirdPartyRedirectURI},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              thirdPartyClientName,
		Scopes:                  []string{"openid"},
	}))

	return svc
}

// noRedirectClient stops following redirects so tests can inspect the redirect response
// itself (status + Location) rather than whatever it points at.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func bffAuthorizeQuery(t *testing.T) string {
	t.Helper()
	verifier := oauth2.GenerateVerifier()
	q := url.Values{
		"client_id":             {webClientID},
		"redirect_uri":          {"https://docstore.example.com/auth/callback"},
		"response_type":         {"code"},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(verifier)},
		"code_challenge_method": {"S256"},
		"scope":                 {"openid"},
		"state":                 {"bff-state"},
	}
	return q.Encode()
}

func thirdPartyAuthorizeQuery(t *testing.T) string {
	t.Helper()
	verifier := oauth2.GenerateVerifier()
	q := url.Values{
		"client_id":             {thirdPartyClientID},
		"redirect_uri":          {thirdPartyRedirectURI},
		"response_type":         {"code"},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(verifier)},
		"code_challenge_method": {"S256"},
		"scope":                 {"openid"},
		"state":                 {"third-party-state"},
	}
	return q.Encode()
}

func TestMount_BFFClientBypassesConsent(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := noRedirectClient()
	resp, err := client.Get(ts.URL + "/oauth/authorize?" + bffAuthorizeQuery(t))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.NotContains(t, string(body), consentFormMarker)
	require.Equal(t, http.StatusFound, resp.StatusCode, "docstore-web should be redirected upstream, not shown a consent page: body=%s", body)
}

func TestMount_UnknownClientShowsConsentPage(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/oauth/authorize?" + thirdPartyAuthorizeQuery(t))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), consentFormMarker)
	require.Contains(t, string(body), thirdPartyClientName)
}

func TestMount_ApproveConsentThenReplayBypassesPage(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	csrfCookie, csrf := getConsentPage(t, client, ts.URL, query)

	req := newConsentPost(t, ts.URL, query, csrf, csrfCookie)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/oauth/authorize?"+query, resp.Header.Get("Location"))

	var consentCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == consentCookieName {
			consentCookie = c
		}
	}
	require.NotNil(t, consentCookie)

	req2, err := http.NewRequest(http.MethodGet, ts.URL+"/oauth/authorize?"+query, nil)
	require.NoError(t, err)
	req2.AddCookie(consentCookie)
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)

	require.NotContains(t, string(body2), consentFormMarker, "an approved client_id covered by a valid consent cookie must not be shown the consent page again")
}

// newConsentPost builds a POST /oauth/consent request approving query, carrying token in the
// form's csrf field and csrfCookie as the browser cookie. Any of token/csrfCookie may be nil
// to exercise the missing-cookie / bad-token branches.
func newConsentPost(t *testing.T, baseURL, query, token string, csrfCookie *http.Cookie) *http.Request {
	t.Helper()
	form := url.Values{"authorize_query": {query}, "csrf": {token}, "decision": {"approve"}}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/oauth/consent", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrfCookie != nil {
		req.AddCookie(csrfCookie)
	}
	return req
}

func TestMount_TamperedConsentCookieIsRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	csrfCookie, csrf := getConsentPage(t, client, ts.URL, query)

	resp, err := client.Do(newConsentPost(t, ts.URL, query, csrf, csrfCookie))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	var consentCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == consentCookieName {
			consentCookie = c
		}
	}
	require.NotNil(t, consentCookie)

	tampered := *consentCookie
	// Flip the cookie's last character. Whatever it decodes to, it will not match the HMAC
	// computed over whatever "c" map results (or fail to parse at all) — either way it must
	// be treated as no consent granted.
	tampered.Value = flipLastChar(tampered.Value)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/oauth/authorize?"+query, nil)
	require.NoError(t, err)
	req.AddCookie(&tampered)
	resp2, err := client.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp2.StatusCode)
	require.Contains(t, string(body2), consentFormMarker, "a tampered consent cookie must not grant consent")
}

func flipLastChar(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	last := b[len(b)-1]
	if last == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	return string(b)
}

func TestMount_ConsentSubmitBadCSRFRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	csrfCookie, _ := getConsentPage(t, client, ts.URL, query)

	// A valid CSRF cookie but a garbage token: the token doesn't match the HMAC bound to the
	// cookie, so the POST must be rejected.
	badToken := "0000000000000000000000000000000000000000000000000000000000000000"
	resp, err := client.Do(newConsentPost(t, ts.URL, query, badToken, csrfCookie))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMount_ConsentSubmitDifferentCSRFCookieRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	// Token minted against one browser's CSRF cookie...
	_, csrf := getConsentPage(t, client, ts.URL, query)
	// ...submitted with a DIFFERENT browser's CSRF cookie (the cross-browser/replay case the
	// original query+day-only token would have allowed). Must be rejected.
	otherCookie := &http.Cookie{Name: csrfCookieName, Value: "some-other-browsers-csrf-cookie-value"}

	resp, err := client.Do(newConsentPost(t, ts.URL, query, csrf, otherCookie))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Empty(t, resp.Cookies(), "a rejected consent POST must not set a consent cookie")
}

func TestMount_ConsentSubmitNoCSRFCookieRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	_, csrf := getConsentPage(t, client, ts.URL, query)

	// Valid token but NO ds_oauth_csrf cookie: with nothing to bind the token to, the POST
	// cannot be trusted.
	resp, err := client.Do(newConsentPost(t, ts.URL, query, csrf, nil))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMount_ConsentSubmitCrossOriginRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	query := thirdPartyAuthorizeQuery(t)
	client := noRedirectClient()
	csrfCookie, csrf := getConsentPage(t, client, ts.URL, query)

	// Even with a valid cookie+token pair, a present cross-origin Origin header is rejected as
	// belt-and-suspenders against CSRF.
	req := newConsentPost(t, ts.URL, query, csrf, csrfCookie)
	req.Header.Set("Origin", "https://attacker.example.com")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMount_WellKnownMetadataPassesThrough(t *testing.T) {
	svc := newMountTestService(t, false)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		var doc map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
		resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "path=%s", path)
		require.Equal(t, "https://docstore.example.com", doc["issuer"], "path=%s", path)
		methods, _ := doc["code_challenge_methods_supported"].([]any)
		require.Contains(t, methods, "S256", "path=%s", path)
	}

	resp, err := http.Get(ts.URL + "/.well-known/jwks.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&jwks))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, jwks.Keys, 1)
}

func TestMount_RegisterPassesThroughInOpenMode(t *testing.T) {
	svc := newMountTestService(t, true)
	mux := http.NewServeMux()
	svc.Mount(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// A public IP literal (rather than a hostname) is used deliberately: RegisterClientV2
	// performs strict DNS resolution of redirect_uri hostnames (rejecting ones that fail to
	// resolve, or resolve to a private address), and this test suite runs with no network
	// access by design (see CLAUDE.md) — an IP literal needs no DNS lookup and so is
	// validated the same way regardless of network availability.
	body := strings.NewReader(`{"redirect_uris":["https://93.184.216.34/callback"],"client_name":"New Client"}`)
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, resp.StatusCode)
}
