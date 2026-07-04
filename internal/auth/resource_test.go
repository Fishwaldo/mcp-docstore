// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/logtest"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// stubVerifier is a Verifier test double: it maps raw token strings to pre-set claims (an
// unknown token is "invalid"). NewResourceVerifier's job under test is identity resolution
// and logging, not token parsing, so a stub keeps the test focused on that wrapper.
type stubVerifier struct{ byToken map[string]*Claims }

func (s stubVerifier) Verify(_ context.Context, raw string) (*Claims, error) {
	c, ok := s.byToken[raw]
	if !ok {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

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
	exp := time.Now().Add(time.Hour)
	stub := stubVerifier{byToken: map[string]*Claims{
		"onboarded": {Subject: "s1", Email: "alice@acme.com", Groups: []string{"eng"}, Expiry: exp},
		"unknown":   {Subject: "s2", Email: "x@nope.com", Expiry: exp},
	}}
	res, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)
	st := newAuthStore(t)
	logger, buf := logtest.New()
	verifier := NewResourceVerifier(stub, res, st, logger, "")

	ti, err := verifier(ctx, "onboarded", &http.Request{RemoteAddr: "203.0.113.5:5000"})
	require.NoError(t, err)
	require.NotEmpty(t, ti.UserID)
	require.False(t, ti.Expiration.IsZero())
	require.Equal(t, "203.0.113.5", ClientIPFromTokenInfo(ti))

	id, ok := IdentityFromTokenInfo(ti)
	require.True(t, ok)
	require.True(t, id.IsAdmin)
	require.Equal(t, []string{"eng"}, id.Groups)

	okRec := logtest.Find(buf, "auth ok")
	require.NotNil(t, okRec)
	require.Equal(t, "DEBUG", okRec["level"])
	require.Equal(t, "203.0.113.5", okRec["client_ip"])

	// A valid token whose email resolves to no tenant -> ErrInvalidToken + a WARN auth-failed event.
	_, err = verifier(ctx, "unknown", &http.Request{RemoteAddr: "203.0.113.6:6000"})
	require.ErrorIs(t, err, mcpauth.ErrInvalidToken)

	failRec := logtest.Find(buf, "auth failed")
	require.NotNil(t, failRec)
	require.Equal(t, "WARN", failRec["level"])
	require.Equal(t, "email_not_onboarded", failRec["reason"])
}
