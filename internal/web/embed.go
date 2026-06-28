// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import "embed"

// distFS holds the built SPA. The committed dist/index.html is a placeholder; the real
// Vite bundle is produced by the frontend build (CI/Docker/make) and overwrites dist/.
//
//go:embed all:dist
var distFS embed.FS
