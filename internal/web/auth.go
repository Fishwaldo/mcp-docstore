// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

const oauthCookieName = "ds_oauth"

// HandleLogin starts the Authorization Code + PKCE flow: it generates a state and PKCE
// verifier, stores them in a short-lived httpOnly cookie, and redirects to the provider.
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := uuid.NewString()
	verifier := oauth2.GenerateVerifier()
	setCookie(w, oauthCookieName, state+"|"+verifier, s.cfg.CookieSecure, 10*time.Minute)
	http.Redirect(w, r, s.oidc.AuthCodeURL(state, verifier), http.StatusFound)
}

// HandleCallback completes the flow: validate state, exchange the code, resolve identity,
// persist a session, and set the session + CSRF cookies.
func (s *Server) HandleCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(oauthCookieName)
	if err != nil {
		http.Error(w, "missing oauth state", http.StatusBadRequest)
		return
	}
	clearCookie(w, oauthCookieName, s.cfg.CookieSecure)
	state, verifier, ok := strings.Cut(c.Value, "|")
	if !ok || state == "" || r.URL.Query().Get("state") != state {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	claims, rawID, tok, err := s.oidc.Exchange(r.Context(), code, verifier)
	if err != nil {
		s.log.WarnContext(r.Context(), "auth callback exchange failed", "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	if _, err := auth.ResolveIdentity(r.Context(), s.resolver, s.store, claims); err != nil {
		s.log.WarnContext(r.Context(), "auth callback identity rejected", "email", claims.Email, "error", err)
		http.Error(w, "not authorized", http.StatusForbidden)
		return
	}
	if err := s.createSession(r.Context(), w, claims, rawID, tok); err != nil {
		s.log.ErrorContext(r.Context(), "auth callback session create failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, claims *auth.Claims, rawID string, tok *oauth2.Token) error {
	raw, hash, err := newSessionToken()
	if err != nil {
		return err
	}
	now := time.Now()
	absolute := now.Add(s.cfg.AbsoluteTimeout)
	expires := now.Add(s.cfg.IdleTimeout)
	if expires.After(absolute) {
		expires = absolute
	}
	if _, err := s.store.CreateSession(ctx, store.NewSession{
		TokenHash: hash, Subject: claims.Subject, Email: claims.Email, Groups: claims.Groups,
		IDToken: rawID, AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, TokenExpiry: tok.Expiry,
		LastSeenAt: now, ExpiresAt: expires, AbsoluteExpiresAt: absolute,
	}); err != nil {
		return err
	}
	setCookie(w, s.cfg.CookieName, raw, s.cfg.CookieSecure, s.cfg.AbsoluteTimeout)
	issueCSRFCookie(w, s.cfg.CookieSecure)
	return nil
}

// HandleLogout revokes the session's refresh token at the authorization server, deletes the
// session, and clears the cookies. Revocation is best-effort: RFC 7009 §2.2 has the client
// treat revocation as fire-and-forget, and a failure here must never block the user from
// logging out locally.
func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cfg.CookieName); err == nil && c.Value != "" {
		hash := hashToken(c.Value)
		if sess, err := s.store.SessionByTokenHash(r.Context(), hash); err == nil && sess.RefreshToken != "" {
			s.revokeRefreshToken(r.Context(), sess.RefreshToken)
		}
		_ = s.store.DeleteSessionByTokenHash(r.Context(), hash)
	}
	clearCookie(w, s.cfg.CookieName, s.cfg.CookieSecure)
	clearCookie(w, csrfCookieName, s.cfg.CookieSecure)
	http.Redirect(w, r, "/", http.StatusFound)
}

// revokeRefreshToken POSTs an RFC 7009 revocation request for refreshToken to the
// authorization server, over s.cfg.Transport (in-process; see Config.Transport). Errors are
// logged, not returned: the local session is torn down regardless of whether the AS could be
// reached.
func (s *Server) revokeRefreshToken(ctx context.Context, refreshToken string) {
	form := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {s.cfg.ClientID},
		"client_secret":   {s.cfg.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Issuer+"/oauth/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		s.log.WarnContext(ctx, "logout revoke request build failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Transport: s.cfg.Transport}
	resp, err := client.Do(req)
	if err != nil {
		s.log.WarnContext(ctx, "logout revoke request failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.log.WarnContext(ctx, "logout revoke returned non-200 status", "status", resp.StatusCode)
	}
}
