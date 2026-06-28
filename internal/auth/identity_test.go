// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

func acmeResolver(t *testing.T) *tenant.Resolver {
	t.Helper()
	res, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)
	return res
}

func TestResolveIdentityUnonboardedEmail(t *testing.T) {
	id, err := ResolveIdentity(context.Background(), acmeResolver(t), newAuthStore(t),
		&Claims{Subject: "s", Email: "nobody@example.com"})
	var ie *IdentityError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, "email_not_onboarded", ie.Reason)
	require.NoError(t, ie.Err)
	require.Equal(t, uuid.Nil, id.TenantID)
}

func TestResolveIdentityHappyPath(t *testing.T) {
	id, err := ResolveIdentity(context.Background(), acmeResolver(t), newAuthStore(t),
		&Claims{Subject: "s1", Email: "alice@acme.com", Groups: []string{"eng"}})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id.TenantID)
	require.NotEqual(t, uuid.Nil, id.UserID)
	require.Equal(t, []string{"eng"}, id.Groups)
	require.True(t, id.IsAdmin)
}
