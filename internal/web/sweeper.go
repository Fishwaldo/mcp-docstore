// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"time"
)

// StartSweeper runs a background goroutine that deletes expired sessions every
// cfg.SweepInterval until ctx is cancelled.
func (s *Server) StartSweeper(ctx context.Context) {
	if s.cfg.SweepInterval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(s.cfg.SweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := s.store.DeleteExpiredSessions(ctx, time.Now())
				if err != nil {
					s.log.ErrorContext(ctx, "session sweep failed", "error", err)
					continue
				}
				if n > 0 {
					s.log.DebugContext(ctx, "swept expired sessions", "count", n)
				}
			}
		}
	}()
}
