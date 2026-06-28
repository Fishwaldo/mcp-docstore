// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

type fakeAuth struct {
	authURL string
	claims  *auth.Claims
}

func (f *fakeAuth) AuthCodeURL(state, verifier string) string { return f.authURL + "?state=" + state }
func (f *fakeAuth) Exchange(ctx context.Context, code, verifier string) (*auth.Claims, string, *oauth2.Token, error) {
	return f.claims, "raw-id-token", &oauth2.Token{AccessToken: "at", RefreshToken: "rt"}, nil
}

var sanitizeRe = regexp.MustCompile(`[/# ]`)

func sanitizeName(name string) string {
	return sanitizeRe.ReplaceAllString(name, "_")
}

func newTestServer(t *testing.T, claims *auth.Claims) (*Server, *store.Store) {
	t.Helper()
	dsn := "file:web-" + sanitizeName(t.Name()) + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	st, err := store.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	require.NoError(t, st.Migrate(context.Background()))
	_, err = st.EnsureTenant(context.Background(), "acme", "Acme")
	require.NoError(t, err)

	resolver, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)

	cfg := Config{
		CookieName:      "ds_session",
		CookieSecure:    false,
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	srv := New(cfg, st, resolver, &fakeAuth{authURL: "https://idp/authorize", claims: claims}, nil)
	return srv, st
}

func findCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	// Return an empty cookie so callers can still check Value == "" without nil panic.
	return &http.Cookie{Name: name, Value: ""}
}

func stateFromOAuthCookie(t *testing.T, value string) string {
	t.Helper()
	state, _, ok := strings.Cut(value, "|")
	require.True(t, ok, "ds_oauth cookie must be state|verifier")
	return state
}

func TestLoginSetsStateCookieAndRedirects(t *testing.T) {
	srv, _ := newTestServer(t, &auth.Claims{Subject: "s1", Email: "alice@acme.com", Groups: []string{"eng"}})
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	srv.HandleLogin(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Location"))
	var hasOAuthCookie bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ds_oauth" {
			hasOAuthCookie = true
			require.True(t, c.HttpOnly)
		}
	}
	require.True(t, hasOAuthCookie, "login must set the ds_oauth state cookie")
}

func TestCallbackCreatesSessionAndRedirects(t *testing.T) {
	srv, st := newTestServer(t, &auth.Claims{Subject: "s1", Email: "alice@acme.com", Groups: []string{"eng"}})
	// First hit login to obtain the ds_oauth cookie (state+verifier).
	loginRec := httptest.NewRecorder()
	srv.HandleLogin(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	oauthCookie := findCookie(t, loginRec, "ds_oauth")
	state := stateFromOAuthCookie(t, oauthCookie.Value)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	req.AddCookie(oauthCookie)
	rec := httptest.NewRecorder()
	srv.HandleCallback(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/", rec.Header().Get("Location"))

	sessionCookie := findCookie(t, rec, "ds_session")
	require.NotEmpty(t, sessionCookie.Value)
	// The session row exists under the hash of the cookie value.
	_, err := st.SessionByTokenHash(context.Background(), hashToken(sessionCookie.Value))
	require.NoError(t, err)
}

func TestCallbackRejectsStateMismatch(t *testing.T) {
	srv, _ := newTestServer(t, &auth.Claims{Subject: "s1", Email: "alice@acme.com"})
	loginRec := httptest.NewRecorder()
	srv.HandleLogin(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	oauthCookie := findCookie(t, loginRec, "ds_oauth")
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=WRONG", nil)
	req.AddCookie(oauthCookie)
	rec := httptest.NewRecorder()
	srv.HandleCallback(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLogoutDeletesSessionAndClearsCookie(t *testing.T) {
	srv, st := newTestServer(t, &auth.Claims{Subject: "s1", Email: "alice@acme.com"})
	// create a session via callback first
	loginRec := httptest.NewRecorder()
	srv.HandleLogin(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	oauthCookie := findCookie(t, loginRec, "ds_oauth")
	state := stateFromOAuthCookie(t, oauthCookie.Value)
	cbRec := httptest.NewRecorder()
	cbReq := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	cbReq.AddCookie(oauthCookie)
	srv.HandleCallback(cbRec, cbReq)
	sessionCookie := findCookie(t, cbRec, "ds_session")

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	srv.HandleLogout(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)
	_, err := st.SessionByTokenHash(context.Background(), hashToken(sessionCookie.Value))
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Empty(t, findCookie(t, rec, "ds_session").Value)
}
