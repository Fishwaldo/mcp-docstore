// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package assets embeds static assets served by the MCP server (e.g. the icon).
package assets

import _ "embed"

//go:embed icon-512.png
var Icon512PNG []byte

//go:embed icon-96.png
var Icon96PNG []byte
