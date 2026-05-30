// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// confirm returns nil if a destructive action is approved. If the client supports
// elicitation it asks (message describes the impact), approving only on action "accept".
// Otherwise it requires the caller to have passed confirm=true. Decline/cancel, or a
// missing confirm, returns an error explaining how to proceed.
func confirm(ctx context.Context, req *sdk.CallToolRequest, message string, confirmArg bool) error {
	if clientSupportsElicitation(req) {
		res, err := req.Session.Elicit(ctx, &sdk.ElicitParams{
			Mode:    "form",
			Message: message,
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
		if err != nil {
			return fmt.Errorf("confirmation failed: %w", err)
		}
		if res.Action != "accept" {
			return fmt.Errorf("action cancelled by user")
		}
		return nil
	}
	if !confirmArg {
		return fmt.Errorf("%w: this action is destructive; retry with confirm=true to proceed", store.ErrInvalid)
	}
	return nil
}

func clientSupportsElicitation(req *sdk.CallToolRequest) bool {
	ip := req.Session.InitializeParams()
	return ip != nil && ip.Capabilities != nil && ip.Capabilities.Elicitation != nil
}
