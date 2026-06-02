// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package logtest provides a slog logger that captures records for assertions in tests.
package logtest

import (
	"bytes"
	"encoding/json"
	"log/slog"
)

// New returns a DEBUG-level JSON slog logger writing into the returned buffer, so tests can
// assert on emitted records regardless of the level under test.
func New() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

// Records parses each JSON line written to buf into a map. Group attributes (e.g. "args")
// appear as nested maps, matching slog's JSON encoding.
func Records(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// Find returns the first record whose "msg" equals msg, or nil.
func Find(buf *bytes.Buffer, msg string) map[string]any {
	for _, r := range Records(buf) {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}
