// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type ctxKey int

const identityCtxKey ctxKey = iota

// IdentityFromContext returns the identity the session middleware resolved for this request.
func IdentityFromContext(ctx context.Context) (store.Identity, bool) {
	id, ok := ctx.Value(identityCtxKey).(store.Identity)
	return id, ok
}

// RequireSession authenticates a request from its session cookie. It enforces the idle and
// absolute TTLs, slides the idle window, re-resolves identity (tenant + admin live; groups
// from the stored snapshot), and attaches the identity to the request context. Any failure
// is a 401 — the SPA treats that as "redirect to login".
func (s *Server) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cfg.CookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		sess, err := s.store.SessionByTokenHash(r.Context(), hashToken(c.Value))
		if err != nil {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		now := time.Now()
		idleDeadline := sess.LastSeenAt.Add(s.cfg.IdleTimeout)
		if now.After(sess.ExpiresAt) || now.After(sess.AbsoluteExpiresAt) || now.After(idleDeadline) {
			_ = s.store.DeleteSessionByTokenHash(r.Context(), hashToken(c.Value))
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		id, err := auth.ResolveIdentity(r.Context(), s.resolver, s.store, &auth.Claims{
			Subject: sess.Subject, Email: sess.Email, Groups: sess.Groups,
		})
		if err != nil {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		newExpires := now.Add(s.cfg.IdleTimeout)
		if newExpires.After(sess.AbsoluteExpiresAt) {
			newExpires = sess.AbsoluteExpiresAt
		}
		_ = s.store.TouchSession(r.Context(), sess.ID, now, newExpires)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityCtxKey, id)))
	})
}
