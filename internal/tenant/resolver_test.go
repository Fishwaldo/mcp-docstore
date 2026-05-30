package tenant

import (
	"testing"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/stretchr/testify/require"
)

func newResolver(t *testing.T) *Resolver {
	t.Helper()
	r, err := NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{
			Domains: []string{"acme.com", "acme.io"},
			Emails:  []string{"contractor@gmail.com"},
		}},
		{Key: "globex", Match: config.TenantMatch{Domains: []string{"globex.com"}}},
	})
	require.NoError(t, err)
	return r
}

func TestResolveByDomain(t *testing.T) {
	r := newResolver(t)
	key, ok := r.Resolve("Alice@ACME.com")
	require.True(t, ok)
	require.Equal(t, "acme", key)
}

func TestExactEmailBeatsDomain(t *testing.T) {
	r := newResolver(t)
	// gmail.com is not a tenant domain, but the exact email is mapped
	key, ok := r.Resolve("contractor@gmail.com")
	require.True(t, ok)
	require.Equal(t, "acme", key)
}

func TestUnknownDomainNotResolved(t *testing.T) {
	r := newResolver(t)
	_, ok := r.Resolve("bob@unknown.org")
	require.False(t, ok)
}

func TestMalformedEmail(t *testing.T) {
	r := newResolver(t)
	_, ok := r.Resolve("not-an-email")
	require.False(t, ok)
}
