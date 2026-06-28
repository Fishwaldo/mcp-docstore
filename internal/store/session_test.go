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

func newTestSessionIn(tokenHash string, now time.Time) NewSession {
	return NewSession{
		TokenHash: tokenHash, Subject: "sub-1", Email: "a@acme.com",
		Groups:  []string{"eng"},
		IDToken: "idt", AccessToken: "at", RefreshToken: "rt",
		TokenExpiry:       now.Add(time.Hour),
		LastSeenAt:        now,
		ExpiresAt:         now.Add(24 * time.Hour),
		AbsoluteExpiresAt: now.Add(7 * 24 * time.Hour),
	}
}

func TestCreateSessionAndLookup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	created, err := s.CreateSession(ctx, newTestSessionIn("hash-1", now))
	require.NoError(t, err)

	got, err := s.SessionByTokenHash(ctx, "hash-1")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "a@acme.com", got.Email)
	require.Equal(t, []string{"eng"}, got.Groups)
	require.Equal(t, "rt", got.RefreshToken)
}

func TestSessionByTokenHashNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.SessionByTokenHash(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTouchSessionSlidesWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	created, err := s.CreateSession(ctx, newTestSessionIn("hash-2", now))
	require.NoError(t, err)

	later := now.Add(time.Hour)
	newExpiry := later.Add(24 * time.Hour)
	require.NoError(t, s.TouchSession(ctx, created.ID, later, newExpiry))

	got, err := s.SessionByTokenHash(ctx, "hash-2")
	require.NoError(t, err)
	require.WithinDuration(t, later, got.LastSeenAt, time.Second)
	require.WithinDuration(t, newExpiry, got.ExpiresAt, time.Second)
}

func TestDeleteSessionByTokenHashIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.CreateSession(ctx, newTestSessionIn("hash-3", time.Now()))
	require.NoError(t, err)
	require.NoError(t, s.DeleteSessionByTokenHash(ctx, "hash-3"))
	_, err = s.SessionByTokenHash(ctx, "hash-3")
	require.ErrorIs(t, err, ErrNotFound)
	// deleting an absent session is not an error
	require.NoError(t, s.DeleteSessionByTokenHash(ctx, "hash-3"))
}
