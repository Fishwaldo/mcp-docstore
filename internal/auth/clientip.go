// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"net"
	"net/http"
	"strings"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// clientIPKey is the TokenInfo.Extra key under which NewResourceVerifier stashes the
// resolved client IP, so the MCP layer can read it the same way it reads identity.
const clientIPKey = "docstore.client_ip"

// ClientIP resolves the caller's IP from r. When header is non-empty (e.g. "X-Forwarded-For")
// it trusts the leftmost entry of that request header, falling back to the connection's
// RemoteAddr when the header is absent or empty. When header is empty it uses RemoteAddr
// directly. Only trust a forwarded header behind a proxy that sets it, since clients can
// otherwise spoof it.
func ClientIP(r *http.Request, header string) string {
	if r == nil {
		return ""
	}
	if header != "" {
		if v := r.Header.Get(header); v != "" {
			if first := strings.TrimSpace(strings.Split(v, ",")[0]); first != "" {
				return first
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ClientIPFromTokenInfo returns the client IP stashed by NewResourceVerifier, or "".
func ClientIPFromTokenInfo(ti *mcpauth.TokenInfo) string {
	if ti == nil || ti.Extra == nil {
		return ""
	}
	ip, _ := ti.Extra[clientIPKey].(string)
	return ip
}
