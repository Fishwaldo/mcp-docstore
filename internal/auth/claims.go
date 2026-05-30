// Package auth validates OAuth bearer tokens and resolves them to a store.Identity.
package auth

import "context"

// Claims is the authenticated subject extracted from a verified token.
type Claims struct {
	Subject string   // the IdP "sub" — globally unique, binds the user to one tenant
	Email   string   // used to resolve the tenant by domain/address
	Groups  []string // from the configured groups claim; drives group shares
}

// Verifier validates a raw bearer token and returns its claims. Implementations must
// fully validate the token (signature, expiry, issuer, audience) before returning.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*Claims, error)
}
