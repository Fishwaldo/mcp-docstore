// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
)

// authClient runs the OIDC Authorization Code + PKCE flow. It is an interface so handler
// tests can substitute a fake without a live IdP token endpoint; the production
// implementation talks to the real provider.
type authClient interface {
	AuthCodeURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (claims *auth.Claims, rawIDToken string, tok *oauth2.Token, err error)
}

type oidcClient struct {
	oauth       *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	groupsClaim string
}

// NewOIDCClient discovers the provider and builds the OAuth + ID-token verifier wiring.
func NewOIDCClient(ctx context.Context, issuer string, cfg Config, groupsClaim string) (authClient, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &oidcClient{
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
		verifier:    provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		groupsClaim: groupsClaim,
	}, nil
}

func (c *oidcClient) AuthCodeURL(state, verifier string) string {
	return c.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (c *oidcClient) Exchange(ctx context.Context, code, verifier string) (*auth.Claims, string, *oauth2.Token, error) {
	tok, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, "", nil, fmt.Errorf("token response missing id_token")
	}
	idt, err := c.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("verify id_token: %w", err)
	}
	var raw map[string]any
	if err := idt.Claims(&raw); err != nil {
		return nil, "", nil, fmt.Errorf("parse id_token claims: %w", err)
	}
	claims := &auth.Claims{
		Subject: idt.Subject,
		Email:   stringClaim(raw, "email"),
		Groups:  stringSliceClaim(raw, c.groupsClaim),
		Expiry:  idt.Expiry,
	}
	return claims, rawID, tok, nil
}

func stringClaim(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func stringSliceClaim(m map[string]any, key string) []string {
	v, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, e := range v {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
