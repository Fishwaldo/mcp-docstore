// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"net/http"
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		header string
		setHdr map[string]string
		remote string
		want   string
	}{
		{name: "remoteaddr strips port", remote: "203.0.113.7:54321", want: "203.0.113.7"},
		{name: "ipv6 remoteaddr", remote: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "no port returns as-is", remote: "203.0.113.7", want: "203.0.113.7"},
		{
			name:   "xff leftmost when header configured",
			header: "X-Forwarded-For",
			setHdr: map[string]string{"X-Forwarded-For": "198.51.100.9, 10.0.0.1"},
			remote: "10.0.0.1:9000",
			want:   "198.51.100.9",
		},
		{
			name:   "falls back to remoteaddr when configured header absent",
			header: "X-Forwarded-For",
			remote: "203.0.113.7:1111",
			want:   "203.0.113.7",
		},
		{
			name:   "ignores xff when header not configured",
			setHdr: map[string]string{"X-Forwarded-For": "1.2.3.4"},
			remote: "203.0.113.7:1111",
			want:   "203.0.113.7",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{Header: http.Header{}, RemoteAddr: tc.remote}
			for k, v := range tc.setHdr {
				r.Header.Set(k, v)
			}
			require.Equal(t, tc.want, ClientIP(r, tc.header))
		})
	}
	require.Equal(t, "", ClientIP(nil, ""))
}

func TestClientIPFromTokenInfo(t *testing.T) {
	require.Equal(t, "", ClientIPFromTokenInfo(nil))
	require.Equal(t, "", ClientIPFromTokenInfo(&mcpauth.TokenInfo{}))
	ti := &mcpauth.TokenInfo{Extra: map[string]any{clientIPKey: "9.9.9.9"}}
	require.Equal(t, "9.9.9.9", ClientIPFromTokenInfo(ti))
}
