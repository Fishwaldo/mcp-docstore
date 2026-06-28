// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
)

const csrfCookieName = "ds_csrf"

// issueCSRFCookie sets a JS-readable CSRF token cookie (double-submit pattern). It is NOT
// httpOnly: the SPA reads it and echoes it in the X-CSRF-Token header on mutations.
func issueCSRFCookie(w http.ResponseWriter, secure bool) {
	raw, _, err := newSessionToken()
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireCSRF enforces the double-submit check on state-changing methods: the X-CSRF-Token
// header must match the ds_csrf cookie. Safe methods pass through.
func (s *Server) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(csrfCookieName)
		if err != nil || c.Value == "" || c.Value != r.Header.Get("X-CSRF-Token") {
			http.Error(w, "csrf token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
