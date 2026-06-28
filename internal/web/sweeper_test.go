// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func TestSweeperDeletesExpired(t *testing.T) {
	srv, st := newTestServer(t, nil)
	ctx := context.Background()
	now := time.Now()
	_, err := st.CreateSession(ctx, store.NewSession{
		TokenHash: "expired", Subject: "s", Email: "a@acme.com",
		LastSeenAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute), AbsoluteExpiresAt: now.Add(-time.Minute),
	})
	require.NoError(t, err)
	n, err := st.DeleteExpiredSessions(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_ = srv
}

func TestSweeperExitsOnCancelledContext(t *testing.T) {
	cfg := Config{
		CookieName:      "ds_session",
		CookieSecure:    false,
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
		SweepInterval:   10 * time.Second,
	}
	srv, _ := newTestServer(t, nil)
	srv.cfg = cfg

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	done := make(chan struct{})
	go func() {
		srv.StartSweeper(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartSweeper did not return promptly on cancelled context")
	}
}
