// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import sdk "github.com/modelcontextprotocol/go-sdk/mcp"

// All tools operate on this server's own database/index — a closed world, never external
// entities — so OpenWorldHint is always false.
func ptrBool(b bool) *bool { return &b }

// readOnlyAnno marks a tool that does not modify state.
func readOnlyAnno() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptrBool(false)}
}

// mutatingAnno marks a tool that modifies state additively/recoverably (not destructive;
// edits are version-guarded and snapshotted).
func mutatingAnno() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: ptrBool(false), OpenWorldHint: ptrBool(false)}
}

// destructiveAnno marks a tool whose effect is hard to reverse (delete/restore); these are
// also elicitation-guarded.
func destructiveAnno() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: ptrBool(true), OpenWorldHint: ptrBool(false)}
}
