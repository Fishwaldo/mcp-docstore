// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/logtest"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func toolReq(args string) *sdk.CallToolRequest {
	id := store.Identity{TenantID: uuid.New(), UserID: uuid.New(), IsAdmin: true}
	return &sdk.CallToolRequest{
		Params: &sdk.CallToolParamsRaw{Name: "create_document", Arguments: json.RawMessage(args)},
		Extra:  &sdk.RequestExtra{TokenInfo: auth.NewTokenInfo("u-1", time.Time{}, id, "1.2.3.4")},
	}
}

func TestLoggingMiddlewareToolCall(t *testing.T) {
	logger, buf := logtest.New()
	h := loggingMiddleware(logger)(func(context.Context, string, sdk.Request) (sdk.Result, error) {
		return &sdk.CallToolResult{}, nil
	})
	_, err := h(context.Background(), "tools/call", toolReq(`{"project_id":"p-1","body":"top secret","limit":5}`))
	require.NoError(t, err)

	rec := logtest.Find(buf, "mcp_call")
	require.NotNil(t, rec)
	require.Equal(t, "INFO", rec["level"])
	require.Equal(t, "tools/call", rec["mcp_method"])
	require.Equal(t, "create_document", rec["tool"])
	require.Equal(t, "1.2.3.4", rec["client_ip"])
	require.Equal(t, "ok", rec["outcome"])
	require.Equal(t, true, rec["admin"])
	require.NotEmpty(t, rec["tenant"])

	args, ok := rec["args"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "p-1", args["project_id"])
	require.Equal(t, float64(5), args["limit"])
	require.NotContains(t, args, "body") // free-text content is never logged
}

func TestLoggingMiddlewareToolError(t *testing.T) {
	logger, buf := logtest.New()
	h := loggingMiddleware(logger)(func(context.Context, string, sdk.Request) (sdk.Result, error) {
		return nil, errors.New("boom")
	})
	_, err := h(context.Background(), "tools/call", toolReq(`{"project_id":"p-1"}`))
	require.Error(t, err)

	rec := logtest.Find(buf, "mcp_call")
	require.NotNil(t, rec)
	require.Equal(t, "ERROR", rec["level"])
	require.Equal(t, "error", rec["outcome"])
	require.Equal(t, "boom", rec["error"])
}

func TestLoggingMiddlewareNonToolIsDebug(t *testing.T) {
	logger, buf := logtest.New()
	h := loggingMiddleware(logger)(func(context.Context, string, sdk.Request) (sdk.Result, error) {
		return &sdk.ListToolsResult{}, nil
	})
	_, err := h(context.Background(), "tools/list", &sdk.CallToolRequest{Extra: &sdk.RequestExtra{}})
	require.NoError(t, err)

	rec := logtest.Find(buf, "mcp_call")
	require.NotNil(t, rec)
	require.Equal(t, "DEBUG", rec["level"])
	require.Equal(t, "tools/list", rec["mcp_method"])
	require.NotContains(t, rec, "tool")
}
