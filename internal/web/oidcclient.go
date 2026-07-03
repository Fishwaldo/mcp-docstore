// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
)

// authClient runs the OAuth Authorization Code + PKCE flow. It is an interface so handler
// tests can substitute a fake without a live authorization server; the production
// implementation talks to DocStore's own embedded authorization server (internal/oauthsrv).
type authClient interface {
	AuthCodeURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (claims *auth.Claims, rawIDToken string, tok *oauth2.Token, err error)
}

// accessTokenVerifier validates a DocStore-issued access token and maps it to identity claims.
// Satisfied by *auth.LocalVerifier; declared here (rather than imported) so this file only
// depends on the one method it calls.
type accessTokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*auth.Claims, error)
}

// asClient is the BFF's OAuth client against DocStore's own embedded authorization server.
// Unlike the upstream-IdP client it replaces, there is no separate OIDC id_token to verify:
// the AS mints RFC 9068 JWT access tokens directly, so Exchange verifies tok.AccessToken itself
// against the injected verifier — no JWKS fetch, no dependency on our own public URL being
// reachable from inside the process for that step.
type asClient struct {
	oauth     *oauth2.Config
	verifier  accessTokenVerifier
	transport http.RoundTripper
}

// NewASClient builds an authClient for issuer, DocStore's own authorization server. Browser-
// facing URLs (AuthCodeURL) point at issuer directly since the browser must hop there over the
// real network; transport carries only the server-to-server Exchange call, so the BFF process
// never needs to dial its own public URL to complete a login.
func NewASClient(issuer, clientID, clientSecret, redirectURL string, verifier accessTokenVerifier, transport http.RoundTripper) *asClient {
	return &asClient{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  issuer + "/oauth/authorize",
				TokenURL: issuer + "/oauth/token",
			},
			RedirectURL: redirectURL,
			Scopes:      []string{"openid", "profile", "email", "groups", "offline_access"},
		},
		verifier:  verifier,
		transport: transport,
	}
}

func (c *asClient) AuthCodeURL(state, verifier string) string {
	return c.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (c *asClient) Exchange(ctx context.Context, code, verifier string) (*auth.Claims, string, *oauth2.Token, error) {
	if c.transport != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Transport: c.transport})
	}
	tok, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", nil, fmt.Errorf("token exchange: %w", err)
	}
	claims, err := c.verifier.Verify(ctx, tok.AccessToken)
	if err != nil {
		return nil, "", nil, fmt.Errorf("verify access token: %w", err)
	}
	return claims, tok.AccessToken, tok, nil
}
