// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
)

// safeArgKeys are the tool-argument keys whose scalar values are safe to log: identifiers,
// versions, modes, limits, and confirmation flags. Free-text keys (name, title, overview,
// body, content, query, tags, heading) are excluded by omission so tenant content and
// search queries never reach the log.
var safeArgKeys = map[string]bool{
	"project_id":        true,
	"document_id":       true,
	"target_project_id": true,
	"base_version":      true,
	"version":           true,
	"mode":              true,
	"limit":             true,
	"offset":            true,
	"confirm":           true,
	"archived":          true,
	"include_archived":  true,
}

// loggingMiddleware returns SDK receiving middleware that emits one structured "mcp_call"
// event per MCP method: the method, the tool name (for tools/call), caller identity, client
// IP, outcome, latency, and any error. Tool calls log at INFO and other methods at DEBUG;
// errors escalate (tool errors to ERROR, others to WARN).
func loggingMiddleware(log *slog.Logger) sdk.Middleware {
	if log == nil {
		log = slog.Default()
	}
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			start := time.Now()
			res, err := next(ctx, method, req)
			log.LogAttrs(ctx, callLevel(method, err), "mcp_call", callAttrs(method, req, err, time.Since(start))...)
			return res, err
		}
	}
}

func callLevel(method string, err error) slog.Level {
	switch {
	case err != nil && method == "tools/call":
		return slog.LevelError
	case err != nil:
		return slog.LevelWarn
	case method == "tools/call":
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

func callAttrs(method string, req sdk.Request, err error, dur time.Duration) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("mcp_method", method),
		slog.Int64("dur_ms", dur.Milliseconds()),
	}
	// ServerSession.ID panics on a nil receiver, so assert the concrete type and check it.
	if ss, ok := req.GetSession().(*sdk.ServerSession); ok && ss != nil {
		if id := ss.ID(); id != "" {
			attrs = append(attrs, slog.String("session", id))
		}
	}
	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		if id, ok := auth.IdentityFromTokenInfo(extra.TokenInfo); ok {
			attrs = append(attrs,
				slog.String("tenant", id.TenantID.String()),
				slog.String("user", id.UserID.String()),
			)
			if id.IsAdmin {
				attrs = append(attrs, slog.Bool("admin", true))
			}
		}
		if ip := auth.ClientIPFromTokenInfo(extra.TokenInfo); ip != "" {
			attrs = append(attrs, slog.String("client_ip", ip))
		}
	}
	if method == "tools/call" {
		if p, ok := req.GetParams().(*sdk.CallToolParamsRaw); ok {
			attrs = append(attrs, slog.String("tool", p.Name))
			if g := safeArgs(p.Arguments); len(g) > 0 {
				attrs = append(attrs, slog.Attr{Key: "args", Value: slog.GroupValue(g...)})
			}
		}
	}
	if err != nil {
		attrs = append(attrs, slog.String("outcome", "error"), slog.String("error", err.Error()))
	} else {
		attrs = append(attrs, slog.String("outcome", "ok"))
	}
	return attrs
}

// safeArgs extracts only allowlisted, scalar argument values from raw tool-call arguments.
func safeArgs(raw json.RawMessage) []slog.Attr {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var attrs []slog.Attr
	for k := range safeArgKeys {
		rv, ok := m[k]
		if !ok {
			continue
		}
		if v, ok := scalar(rv); ok {
			attrs = append(attrs, slog.Any(k, v))
		}
	}
	return attrs
}

// scalar decodes raw into a string/number/bool, rejecting arrays, objects, and null.
func scalar(raw json.RawMessage) (any, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	switch v.(type) {
	case string, float64, bool:
		return v, true
	default:
		return nil, false
	}
}
