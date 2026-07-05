// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"strings"

	"github.com/klauspost/compress/gzhttp"
)

// compress wraps next so responses are gzip- or zstd-compressed according to the
// client's Accept-Encoding, with gzhttp handling negotiation, a minimum-size
// threshold, and skipping already-compressed content types.
//
// The MCP endpoint is deliberately excluded: Streamable HTTP holds long-lived SSE
// response streams that must flush event-by-event, and routing them through a
// compressor would buffer the stream and break it. Every other route — the SPA
// bundle, the JSON /api, and the /oauth + /.well-known metadata — is compressible.
func compress(next http.Handler) (http.Handler, error) {
	wrapper, err := gzhttp.NewWrapper()
	if err != nil {
		return nil, err
	}
	compressed := wrapper(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/") {
			next.ServeHTTP(w, r)
			return
		}
		compressed.ServeHTTP(w, r)
	}), nil
}
