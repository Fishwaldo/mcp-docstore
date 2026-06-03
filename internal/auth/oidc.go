// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier validates tokens against an OIDC provider discovered at the issuer URL.
type OIDCVerifier struct {
	verifier            *oidc.IDTokenVerifier
	emailClaim          string
	groupsClaim         string
	emailVerifiedPolicy string
}

// NewOIDCVerifier discovers an OIDC provider and builds a verifier that requires the
// given audience. emailClaim/groupsClaim name the claims to extract (defaults
// "email"/"groups" supplied by config). When discoveryURL is empty, standard OIDC
// discovery runs against issuer (issuer + /.well-known/openid-configuration); when set,
// the provider metadata is fetched from discoveryURL instead, for providers that publish
// it at an off-spec path (e.g. RFC 8414 /.well-known/oauth-authorization-server).
// discoveryTimeout bounds the discovery HTTP calls and is also installed on the client the
// provider reuses for later JWKS key refreshes, so a hung IdP cannot block construction or
// token verification indefinitely.
func NewOIDCVerifier(ctx context.Context, issuer, discoveryURL, audience, emailClaim, groupsClaim, emailVerifiedPolicy string, discoveryTimeout time.Duration) (*OIDCVerifier, error) {
	// Fail fast on an empty audience: with ClientID="" and SkipClientIDCheck=false the
	// aud check would error on every Verify at runtime instead of here at construction.
	if audience == "" {
		return nil, fmt.Errorf("oidc verifier: audience is required")
	}
	// Bound discovery + JWKS refresh: a hung IdP must not block startup or verification.
	httpClient := &http.Client{Timeout: discoveryTimeout}
	ctx = oidc.ClientContext(ctx, httpClient)
	dctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	var provider *oidc.Provider
	var err error
	if discoveryURL == "" {
		provider, err = oidc.NewProvider(dctx, issuer)
	} else {
		provider, err = providerFromMetadataURL(dctx, issuer, discoveryURL, httpClient)
	}
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	cfg := &oidc.Config{
		ClientID: audience, // verifies the "aud" claim
		// Asymmetric algorithms only — excludes HS*/none (signature-confusion / forgery).
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256, oidc.PS256, oidc.EdDSA},
	}
	return &OIDCVerifier{
		verifier:            provider.Verifier(cfg),
		emailClaim:          emailClaim,
		groupsClaim:         groupsClaim,
		emailVerifiedPolicy: emailVerifiedPolicy,
	}, nil
}

// providerFromMetadataURL builds a provider from a metadata document fetched directly
// from metadataURL, bypassing the standard discovery path. It asserts the document's
// "issuer" equals the configured issuer (RFC 8414 / OIDC require them to match, and
// unlike oidc.NewProvider, oidc.ProviderConfig does not enforce it) so a swapped or
// misconfigured metadata document can't silently point token verification at the wrong
// signing keys.
func providerFromMetadataURL(ctx context.Context, issuer, metadataURL string, httpClient *http.Client) (*oidc.Provider, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build metadata request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch metadata: %s returned %s", metadataURL, resp.Status)
	}
	var pc oidc.ProviderConfig
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	if pc.IssuerURL != issuer {
		return nil, fmt.Errorf("metadata issuer %q does not match configured issuer %q", pc.IssuerURL, issuer)
	}
	return pc.NewProvider(ctx), nil
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
	if idToken.Subject == "" {
		return nil, fmt.Errorf("verify token: missing subject")
	}
	email, _ := raw[v.emailClaim].(string)
	claims := &Claims{
		Subject:       idToken.Subject,
		Email:         email,
		Groups:        toStringSlice(raw[v.groupsClaim]),
		Expiry:        idToken.Expiry,
		EmailVerified: parseEmailVerified(raw["email_verified"]),
	}
	if err := v.enforceEmailVerified(claims.EmailVerified); err != nil {
		return nil, err
	}
	return claims, nil
}

// enforceEmailVerified applies the configured policy to the decoded email_verified value:
//
//	"require"    — present and true, else reject;
//	"if_present" — reject only when present and false;
//	"off" (or any unknown value) — never reject.
func (v *OIDCVerifier) enforceEmailVerified(verified *bool) error {
	switch v.emailVerifiedPolicy {
	case "require":
		if verified == nil || !*verified {
			return fmt.Errorf("verify token: email not verified")
		}
	case "if_present":
		if verified != nil && !*verified {
			return fmt.Errorf("verify token: email not verified")
		}
	}
	return nil
}

// parseEmailVerified coerces a JSON "email_verified" claim into *bool, tolerating both
// a JSON boolean and a stringified "true"/"false" (some IdPs emit the latter). It returns
// nil when the claim is absent or has an unrecognized shape, leaving the policy to decide.
func parseEmailVerified(v any) *bool {
	switch t := v.(type) {
	case bool:
		return &t
	case string:
		switch t {
		case "true":
			b := true
			return &b
		case "false":
			b := false
			return &b
		}
	}
	return nil
}

// toStringSlice coerces a JSON claim value (which decodes as []any) into []string,
// tolerating a single string or a missing/non-string element.
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
	case string:
		return []string{t}
	default:
		return nil
	}
}
