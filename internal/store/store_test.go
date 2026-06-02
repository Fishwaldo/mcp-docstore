// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"testing"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	"github.com/stretchr/testify/require"
)

// newTestStore returns a migrated in-memory store for tests.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAndMigrate(t *testing.T) {
	s := newTestStore(t)
	require.NotNil(t, s.client)
}

func TestUpsertTenantAndUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ten, err := s.EnsureTenant(ctx, "acme", "Acme Corp")
	require.NoError(t, err)
	require.Equal(t, "acme", ten.Key)

	// First upsert creates the user.
	u1, err := s.UpsertUser(ctx, ten.ID, "sub-123", "alice@acme.com", false)
	require.NoError(t, err)
	require.Equal(t, "alice@acme.com", u1.Email)

	// Second upsert with same subject returns the same user (email refreshed).
	u2, err := s.UpsertUser(ctx, ten.ID, "sub-123", "alice2@acme.com", false)
	require.NoError(t, err)
	require.Equal(t, u1.ID, u2.ID)
	require.Equal(t, "alice2@acme.com", u2.Email)

	// EnsureTenant is idempotent by key.
	ten2, err := s.EnsureTenant(ctx, "acme", "Acme Corp (renamed)")
	require.NoError(t, err)
	require.Equal(t, ten.ID, ten2.ID)
}

// Security-critical invariant: a subject already bound to one tenant cannot be
// re-bound to another (external_subject is globally unique → single-tenant binding).
func TestUpsertUserRejectsCrossTenantRebinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenA, err := s.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	tenB, err := s.EnsureTenant(ctx, "globex", "Globex")
	require.NoError(t, err)

	_, err = s.UpsertUser(ctx, tenA.ID, "sub-shared", "x@acme.com", false)
	require.NoError(t, err)

	// Same subject, different tenant → rejected with ErrInvalid, nothing rebound.
	_, err = s.UpsertUser(ctx, tenB.ID, "sub-shared", "x@globex.com", false)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestUpsertUserReconcilesAdminRole(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	ten, err := st.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)

	u, err := st.UpsertUser(ctx, ten.ID, "sub-1", "alice@acme.com", true)
	require.NoError(t, err)
	require.Equal(t, user.RoleAdmin, u.Role)

	u, err = st.UpsertUser(ctx, ten.ID, "sub-1", "alice@acme.com", false)
	require.NoError(t, err)
	require.Equal(t, user.RoleMember, u.Role)
}

// The concurrent-first-login branch in UpsertUser (User.Create hits the unique
// external_subject constraint, then re-queries and reconciles the existing row) cannot be
// forced deterministically in a single-connection in-memory test: it requires two
// simultaneous Creates racing on the same subject. Its correctness rests on the unique
// index on external_subject plus the shared reconciliation path exercised by
// TestUpsertTenantAndUser and TestUpsertUserReconcilesAdminRole, and is verified by review.
func TestUpsertUserRejectsEmptySubject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ten, err := s.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)

	_, err = s.UpsertUser(ctx, ten.ID, "", "alice@acme.com", false)
	require.ErrorIs(t, err, ErrInvalid)
}
