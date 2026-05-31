// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerInfoAndInstructions(t *testing.T) {
	svc, _, id, _ := newSvc(t)
	cs := startServer(t, svc, id)

	init := cs.InitializeResult()
	require.NotNil(t, init)
	require.NotNil(t, init.ServerInfo)
	require.Equal(t, "mcp-docstore", init.ServerInfo.Name)
	require.Equal(t, "MCP DocStore", init.ServerInfo.Title)
	// startServer passes "test" as the build version; it must be advertised verbatim.
	require.Equal(t, "test", init.ServerInfo.Version)

	// Instructions are advertised and mention the core model concepts.
	require.NotEmpty(t, init.Instructions)
	for _, want := range []string{"overview", "base_version", "search_documents", "confirm"} {
		require.Truef(t, strings.Contains(init.Instructions, want), "instructions should mention %q", want)
	}
}
