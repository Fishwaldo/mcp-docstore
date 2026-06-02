// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package auth validates OAuth bearer tokens and resolves them to a store.Identity.
package auth

import (
	"context"
	"time"
)

// Claims is the authenticated subject extracted from a verified token.
type Claims struct {
	Subject string    // the IdP "sub" — globally unique, binds the user to one tenant
	Email   string    // used to resolve the tenant by domain/address
	Groups  []string  // from the configured groups claim; drives group shares
	Expiry  time.Time // token "exp"; required by the SDK bearer middleware
	// EmailVerified is the decoded "email_verified" claim: nil when the claim is
	// absent, else the decoded boolean. The email_verified policy decides whether
	// an absent or false value rejects the token.
	EmailVerified *bool
}

// Verifier validates a raw bearer token and returns its claims. Implementations must
// fully validate the token (signature, expiry, issuer, audience) before returning.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*Claims, error)
}
