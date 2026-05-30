// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantByKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)

	got, err := s.TenantByKey(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	_, err = s.TenantByKey(ctx, "ghost")
	require.ErrorIs(t, err, ErrNotFound)
}
