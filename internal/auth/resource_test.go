// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/logtest"
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
	ov, err := NewOIDCVerifier(ctx, issuer, "", "mcp-docstore", "email", "groups")
	require.NoError(t, err)
	res, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)
	st := newAuthStore(t)
	logger, buf := logtest.New()
	verifier := NewResourceVerifier(ov, res, st, logger, "")

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	tok := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s1","exp":` + exp +
		`,"email":"alice@acme.com","groups":["eng"]}`)
	ti, err := verifier(ctx, tok, &http.Request{RemoteAddr: "203.0.113.5:5000"})
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

	// Unknown domain -> ErrInvalidToken + a WARN auth-failed event.
	tok2 := sign(`{"iss":"` + issuer + `","aud":"mcp-docstore","sub":"s2","exp":` + exp + `,"email":"x@nope.com"}`)
	_, err = verifier(ctx, tok2, &http.Request{RemoteAddr: "203.0.113.6:6000"})
	require.ErrorIs(t, err, mcpauth.ErrInvalidToken)

	failRec := logtest.Find(buf, "auth failed")
	require.NotNil(t, failRec)
	require.Equal(t, "WARN", failRec["level"])
	require.Equal(t, "email_not_onboarded", failRec["reason"])
}
