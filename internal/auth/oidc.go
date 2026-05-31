// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier validates tokens against an OIDC provider discovered at the issuer URL.
type OIDCVerifier struct {
	verifier    *oidc.IDTokenVerifier
	emailClaim  string
	groupsClaim string
}

// NewOIDCVerifier discovers an OIDC provider and builds a verifier that requires the
// given audience. emailClaim/groupsClaim name the claims to extract (defaults
// "email"/"groups" supplied by config). When discoveryURL is empty, standard OIDC
// discovery runs against issuer (issuer + /.well-known/openid-configuration); when set,
// the provider metadata is fetched from discoveryURL instead, for providers that publish
// it at an off-spec path (e.g. RFC 8414 /.well-known/oauth-authorization-server).
func NewOIDCVerifier(ctx context.Context, issuer, discoveryURL, audience, emailClaim, groupsClaim string) (*OIDCVerifier, error) {
	// Fail fast on an empty audience: with ClientID="" and SkipClientIDCheck=false the
	// aud check would error on every Verify at runtime instead of here at construction.
	if audience == "" {
		return nil, fmt.Errorf("oidc verifier: audience is required")
	}
	var provider *oidc.Provider
	var err error
	if discoveryURL == "" {
		provider, err = oidc.NewProvider(ctx, issuer)
	} else {
		provider, err = providerFromMetadataURL(ctx, issuer, discoveryURL)
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
		verifier:    provider.Verifier(cfg),
		emailClaim:  emailClaim,
		groupsClaim: groupsClaim,
	}, nil
}

// providerFromMetadataURL builds a provider from a metadata document fetched directly
// from metadataURL, bypassing the standard discovery path. It asserts the document's
// "issuer" equals the configured issuer (RFC 8414 / OIDC require them to match, and
// unlike oidc.NewProvider, oidc.ProviderConfig does not enforce it) so a swapped or
// misconfigured metadata document can't silently point token verification at the wrong
// signing keys.
func providerFromMetadataURL(ctx context.Context, issuer, metadataURL string) (*oidc.Provider, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build metadata request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
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
	email, _ := raw[v.emailClaim].(string)
	return &Claims{
		Subject: idToken.Subject,
		Email:   email,
		Groups:  toStringSlice(raw[v.groupsClaim]),
		Expiry:  idToken.Expiry,
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
