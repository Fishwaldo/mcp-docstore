// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSessionSchemaRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	created, err := s.client.Session.Create().
		SetTokenHash("h1").
		SetSubject("sub-1").
		SetEmail("a@acme.com").
		SetGroups([]string{"eng"}).
		SetLastSeenAt(now).
		SetExpiresAt(now.Add(time.Hour)).
		SetAbsoluteExpiresAt(now.Add(24 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)

	got, err := s.client.Session.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "h1", got.TokenHash)
	require.Equal(t, []string{"eng"}, got.Groups)
}
