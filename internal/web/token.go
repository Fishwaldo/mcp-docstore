// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package web is the BFF transport: it serves the browser UI, runs the OAuth
// Authorization Code flow server-side, and exposes a JSON API over the same app.Service
// the MCP tools use. The browser only ever holds an httpOnly session cookie; all tokens
// stay server-side.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// newSessionToken returns a fresh random session token and its SHA-256 hash. The raw token
// goes in the cookie; only the hash is persisted, so reading the session table cannot mint
// a valid cookie.
func newSessionToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

// hashToken returns the lowercase hex SHA-256 of a raw session token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
