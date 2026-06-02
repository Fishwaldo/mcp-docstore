// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

func newAuthStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("sqlite", "file:authres-"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	require.NoError(t, st.Migrate(context.Background()))
	_, err = st.EnsureTenant(context.Background(), "acme", "Acme")
	require.NoError(t, err)
	return st
}

func TestResourceVerifierResolvesIdentity(t *testing.T) {
	ctx := context.Background()
	issuer, sign := startOIDC(t)
	ov, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups", "off")
	require.NoError(t, err)
	res, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)
	st := newAuthStore(t)
	verifier := NewResourceVerifier(ov, res, st)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s1","exp":` + exp +
		`,"email":"alice@acme.com","groups":["eng"]}`)
	ti, err := verifier(ctx, tok, nil)
	require.NoError(t, err)
	require.NotEmpty(t, ti.UserID)
	require.False(t, ti.Expiration.IsZero())

	id, ok := IdentityFromTokenInfo(ti)
	require.True(t, ok)
	require.True(t, id.IsAdmin)
	require.Equal(t, []string{"eng"}, id.Groups)

	// Unknown domain -> ErrInvalidToken.
	tok2 := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s2","exp":` + exp + `,"email":"x@nope.com"}`)
	_, err = verifier(ctx, tok2, nil)
	require.ErrorIs(t, err, mcpauth.ErrInvalidToken)
}
