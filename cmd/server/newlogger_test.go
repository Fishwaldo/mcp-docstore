// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
)

func TestNewLoggerRespectsLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	log := newLogger(buf, config.Logging{Level: "warn", Format: "json"})
	log.Info("hidden")
	log.Warn("shown")
	out := buf.String()
	require.NotContains(t, out, "hidden")
	require.Contains(t, out, "shown")
	require.Contains(t, out, `"level":"WARN"`)
}

func TestNewLoggerTextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	log := newLogger(buf, config.Logging{Level: "info", Format: "text"})
	log.Info("hi")
	out := buf.String()
	require.Contains(t, out, "level=INFO")
	require.False(t, strings.HasPrefix(strings.TrimSpace(out), "{")) // not JSON
}
