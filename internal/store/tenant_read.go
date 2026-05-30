package store

import (
	"context"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/tenant"
)

// TenantByKey returns the tenant with the given config key, or ErrNotFound if no such
// tenant has been provisioned. Used by the auth layer to map a resolved tenant key to
// its row; tenants are seeded from config at boot.
func (s *Store) TenantByKey(ctx context.Context, key string) (*ent.Tenant, error) {
	t, err := s.client.Tenant.Query().Where(tenant.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return t, err
}
