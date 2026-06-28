// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"io/fs"
	"net/http"
)

// contentSecurityPolicy is the strict CSP for the SPA. script-src is locked to 'self'
// (Vite emits hashed external bundles); style-src allows 'unsafe-inline' because Radix
// sets inline style attributes that a nonce cannot cover; external images are also blocked
// by the server-side sanitizer. This is defense-in-depth over the sanitized render path.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// SPAHandler serves the embedded single-page app. Requests for existing embedded files are
// served directly; anything else returns index.html so the SPA's client-side router can
// handle the path. Every response carries the strict CSP.
func (s *Server) SPAHandler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		p := r.URL.Path
		if p == "/" {
			serveIndex(w, index)
			return
		}
		if _, statErr := fs.Stat(sub, p[1:]); statErr != nil {
			serveIndex(w, index) // unknown path → SPA client route
			return
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}
