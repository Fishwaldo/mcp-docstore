// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package docs

import udiff "github.com/aymanbagabas/go-udiff"

// UnifiedDiff returns a unified diff transforming `from` into `to`. The labels name
// the two sides in the diff header (e.g. "v3" and "v5"). Identical inputs yield "".
func UnifiedDiff(fromLabel, toLabel, from, to string) string {
	return udiff.Unified(fromLabel, toLabel, from, to)
}
