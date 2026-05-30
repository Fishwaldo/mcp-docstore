package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier validates tokens against an OIDC provider discovered at the issuer URL.
type OIDCVerifier struct {
	verifier    *oidc.IDTokenVerifier
	emailClaim  string
	groupsClaim string
}

// NewOIDCVerifier performs OIDC discovery against issuer and builds a verifier that
// requires the given audience. emailClaim/groupsClaim name the claims to extract
// (defaults "email"/"groups" supplied by config).
func NewOIDCVerifier(ctx context.Context, issuer, audience, emailClaim, groupsClaim string) (*OIDCVerifier, error) {
	// Fail fast on an empty audience: with ClientID="" and SkipClientIDCheck=false the
	// aud check would error on every Verify at runtime instead of here at construction.
	if audience == "" {
		return nil, fmt.Errorf("oidc verifier: audience is required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	cfg := &oidc.Config{
		ClientID: audience, // verifies the "aud" claim
		// Asymmetric algorithms only — excludes HS*/none (signature-confusion / forgery).
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256, oidc.PS256, oidc.EdDSA},
	}
	return &OIDCVerifier{
		verifier:    provider.Verifier(cfg),
		emailClaim:  emailClaim,
		groupsClaim: groupsClaim,
	}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	email, _ := raw[v.emailClaim].(string)
	return &Claims{
		Subject: idToken.Subject,
		Email:   email,
		Groups:  toStringSlice(raw[v.groupsClaim]),
	}, nil
}

// toStringSlice coerces a JSON claim value (which decodes as []any) into []string,
// tolerating a single string or a missing/!string element.
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		return []string{t}
	default:
		return nil
	}
}
