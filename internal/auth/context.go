package auth

import (
	"context"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type ctxKey struct{}

// WithIdentity returns a copy of ctx carrying the resolved identity.
func WithIdentity(ctx context.Context, id store.Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFromContext returns the identity injected by the auth middleware. The bool is
// false if the request was not authenticated (no middleware ran or it failed).
func IdentityFromContext(ctx context.Context) (store.Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(store.Identity)
	return id, ok
}
