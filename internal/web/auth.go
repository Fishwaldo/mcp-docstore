// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

const oauthCookieName = "ds_oauth"

// csrfCookieName is the cookie that carries the CSRF token. Task 5 (csrf.go) will finalize
// CSRF handling; this const is a temporary stub so Task 3 builds standalone.
const csrfCookieName = "ds_csrf"

// issueCSRFCookie writes a temporary CSRF cookie. Task 5 replaces this stub with the real
// double-submit implementation in csrf.go.
func issueCSRFCookie(w http.ResponseWriter, secure bool) {
	setCookie(w, csrfCookieName, uuid.NewString(), secure, 12*time.Hour)
}

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

// HandleLogout deletes the current session and clears the cookies.
func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cfg.CookieName); err == nil && c.Value != "" {
		_ = s.store.DeleteSessionByTokenHash(r.Context(), hashToken(c.Value))
	}
	clearCookie(w, s.cfg.CookieName, s.cfg.CookieSecure)
	clearCookie(w, csrfCookieName, s.cfg.CookieSecure)
	dest := s.cfg.PostLogoutRedirectURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}
