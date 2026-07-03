// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"crypto/tls"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/giantswarm/mcp-oauth/security"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // registers "sqlite" driver

	"github.com/Fishwaldo/mcp-docstore/internal/app"
	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/entstore"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/idptest"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// docstorePublicURL uses a public IP literal (example.com's, per RFC 5737 — the same one
// internal/oauthsrv's own mount_test.go TestMount_RegisterPassesThroughInOpenMode uses) rather
// than a hostname: the embedded authorization server performs strict DNS resolution and
// private/link-local IP validation on every redirect_uri at authorization time (defense against
// DNS-rebinding SSRF), and this test suite runs with no network access by design (see
// CLAUDE.md). An IP literal skips DNS resolution entirely and, being a public address, passes
// the private/link-local checks trivially — while hostRoutingTransport (below) guarantees no
// packet is ever actually sent to it: every request to this host is served in-process.
const (
	docstorePublicURL = "https://93.184.216.34"
	docstoreOwnHost   = "93.184.216.34"
)

// testAS bundles a real embedded authorization server — federating to an idptest upstream —
// mounted on mux, plus the seeded first-party BFF client credentials and a LocalVerifier wired
// to its signing key, for driving the web package's login flow against the genuine article
// rather than a fake IdP.
type testAS struct {
	mux          *http.ServeMux
	clientID     string
	clientSecret string
	verifier     *auth.LocalVerifier
	idp          *idptest.Server
}

// newTestASEntClient mirrors internal/oauthsrv's own test helper of the same purpose: a named
// shared in-memory SQLite database (a private ":memory:" DSN would give each pooled connection
// its own empty database).
func newTestASEntClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:web-as-"+sanitizeName(t.Name())+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(context.Background()))
	return client
}

// newTestAS assembles the authorization server and mounts its routes on mux at publicURL.
func newTestAS(t *testing.T, mux *http.ServeMux, publicURL string) *testAS {
	t.Helper()

	idp := idptest.New(t)

	entc := newTestASEntClient(t)
	km, err := oauthsrv.LoadOrCreateKeyMaterial(context.Background(), entc)
	require.NoError(t, err)

	enc, err := security.NewEncryptor([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	combined := entstore.New(entc, enc, 24*time.Hour)

	cfg := oauthsrv.Config{
		PublicURL:            publicURL,
		UpstreamIssuer:       idp.URL,
		UpstreamClientID:     "test-client",
		UpstreamClientSecret: "test-secret",
		UpstreamScopes:       []string{"openid", "profile", "email", "groups"},
		AllowPrivateIP:       true,
		RootCAs:              idp.RootCAs,
		DiscoveryTimeout:     5 * time.Second,
		AccessTokenTTL:       time.Hour,
		RefreshTokenTTL:      90 * 24 * time.Hour,
		RegistrationOpen:     false,
	}

	svc, err := oauthsrv.New(context.Background(), cfg, combined, km, entc, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	clientID, clientSecret, err := svc.SeedBFFClient(context.Background())
	require.NoError(t, err)

	svc.Mount(mux)

	keys, err := svc.PublicKeys()
	require.NoError(t, err)
	verifier := auth.NewLocalVerifier(publicURL, publicURL+"/mcp", keys, combined)

	return &testAS{mux: mux, clientID: clientID, clientSecret: clientSecret, verifier: verifier, idp: idp}
}

// hostRoutingTransport routes requests addressed to ownHost straight to mux in-process (the
// same technique muxTransport uses, scoped to one host among several) and falls through to real
// for every other host — the upstream idptest IdP, reached over its own real TLS listener. It
// plays the role of a browser's network stack in these tests: the browser must really reach the
// upstream IdP, but "reaching" our own public host is simulated in-process rather than
// requiring a real listener.
type hostRoutingTransport struct {
	ownHost string
	mux     http.Handler
	real    http.RoundTripper
}

func (t *hostRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.ownHost {
		rec := httptest.NewRecorder()
		t.mux.ServeHTTP(rec, req)
		return rec.Result(), nil
	}
	return t.real.RoundTrip(req)
}

// flowHarness wires a real embedded AS (federating to idptest) and a real web.Server onto one
// shared mux, plus a "browser" http.Client that reaches the AS's own host in-process and the
// upstream idptest IdP over its real TLS listener.
type flowHarness struct {
	as      *testAS
	store   *store.Store
	webSrv  *Server
	browser *http.Client
	jar     *cookiejar.Jar
}

// newFlowHarness builds the full harness described on flowHarness. idp.User.Email must match a
// tenant resolver domain for HandleCallback's identity resolution to succeed; it does here
// ("example.com", idptest's default user domain).
func newFlowHarness(t *testing.T) *flowHarness {
	t.Helper()

	mux := http.NewServeMux()
	as := newTestAS(t, mux, docstorePublicURL)

	dsn := "file:web-flow-" + sanitizeName(t.Name()) + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	st, err := store.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	require.NoError(t, st.Migrate(context.Background()))
	_, err = st.EnsureTenant(context.Background(), "acme", "Acme")
	require.NoError(t, err)

	resolver, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"example.com"}}, Admins: []string{"user@example.com"}},
	})
	require.NoError(t, err)

	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })
	appSvc := app.NewService(st, index.New(st, idx), nil)

	transport := muxTransport{h: mux}
	client := NewASClient(docstorePublicURL, as.clientID, as.clientSecret, docstorePublicURL+"/auth/callback", as.verifier, transport)

	webCfg := Config{
		ClientID:        as.clientID,
		ClientSecret:    as.clientSecret,
		Issuer:          docstorePublicURL,
		RedirectURL:     docstorePublicURL + "/auth/callback",
		Transport:       transport,
		CookieName:      "ds_session",
		CookieSecure:    false,
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	webSrv := New(webCfg, st, appSvc, resolver, client, slog.New(slog.DiscardHandler))
	webSrv.Mount(mux)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{
		Jar: jar,
		Transport: &hostRoutingTransport{
			ownHost: docstoreOwnHost,
			mux:     mux,
			real:    &http.Transport{TLSClientConfig: &tls.Config{RootCAs: as.idp.RootCAs}},
		},
	}

	return &flowHarness{as: as, store: st, webSrv: webSrv, browser: browser, jar: jar}
}

// login drives GET /auth/login through the full redirect chain (our AS, bypassing consent for
// the first-party BFF; the idptest upstream; back through our AS's own callback; finally the
// BFF's /auth/callback) and returns the ds_session cookie the flow leaves in the jar.
func (h *flowHarness) login(t *testing.T) *http.Cookie {
	t.Helper()
	resp, err := h.browser.Get(docstorePublicURL + "/auth/login")
	require.NoError(t, err)
	resp.Body.Close()

	u, err := url.Parse(docstorePublicURL)
	require.NoError(t, err)
	for _, c := range h.jar.Cookies(u) {
		if c.Name == h.webSrv.cfg.CookieName {
			return c
		}
	}
	return nil
}

// TestFullLoginFlowAgainstEmbeddedAS drives the entire login end to end against a real
// oauthsrv.Service federating to an idptest upstream: GET /auth/login redirects to our own AS's
// /oauth/authorize (bypassing consent — docstore-web is first-party), which redirects the
// browser to the upstream idptest IdP, which auto-approves and redirects back to our AS's own
// /oauth/callback, which exchanges the upstream code and redirects the browser to the BFF's
// /auth/callback with a DocStore-issued authorization code. HandleCallback then exchanges that
// code for a DocStore access token over the in-process transport (never the network) and
// verifies it locally. A subsequent authenticated request to /api/projects must then succeed.
func TestFullLoginFlowAgainstEmbeddedAS(t *testing.T) {
	h := newFlowHarness(t)

	sessionCookie := h.login(t)
	require.NotNil(t, sessionCookie, "login flow must leave a ds_session cookie in the jar")
	require.NotEmpty(t, sessionCookie.Value)

	_, err := h.store.SessionByTokenHash(context.Background(), hashToken(sessionCookie.Value))
	require.NoError(t, err, "session must be persisted under the hash of the cookie value")

	apiResp, err := h.browser.Get(docstorePublicURL + "/api/projects")
	require.NoError(t, err)
	defer apiResp.Body.Close()
	require.Equal(t, http.StatusOK, apiResp.StatusCode, "an authenticated request must reach the API")
}

// TestLogoutRevokesRefreshToken logs in, captures the session's refresh token, then logs out and
// asserts: the local session row is gone, and a subsequent refresh_token grant against the AS's
// own /oauth/token endpoint using that refresh token is rejected — proving HandleLogout's
// revocation call actually reached the AS (over the in-process transport) rather than being a
// no-op.
func TestLogoutRevokesRefreshToken(t *testing.T) {
	h := newFlowHarness(t)

	sessionCookie := h.login(t)
	require.NotNil(t, sessionCookie)

	sess, err := h.store.SessionByTokenHash(context.Background(), hashToken(sessionCookie.Value))
	require.NoError(t, err)
	require.NotEmpty(t, sess.RefreshToken, "the session must have a refresh token to revoke")
	refreshToken := sess.RefreshToken

	logoutResp, err := h.browser.Get(docstorePublicURL + "/auth/logout")
	require.NoError(t, err)
	logoutResp.Body.Close()

	_, err = h.store.SessionByTokenHash(context.Background(), hashToken(sessionCookie.Value))
	require.ErrorIs(t, err, store.ErrNotFound, "logout must delete the local session")

	transport := muxTransport{h: h.as.mux}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {h.as.clientID},
		"client_secret": {h.as.clientSecret},
	}
	req, err := http.NewRequest(http.MethodPost, docstorePublicURL+"/oauth/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusOK, resp.StatusCode, "a revoked refresh token must not mint a new access token")
}
