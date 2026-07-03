// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package oauthsrv is the embedded OAuth 2.1 authorization server built on top of
// github.com/giantswarm/mcp-oauth, with the "entstore" subpackage adapting its storage
// interfaces to the same ent-backed database the rest of mcp-docstore uses.
package oauthsrv

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

// HKDF info strings identify each secret derived from the stored master secret. They are
// fixed forever: changing one would silently rotate that secret (invalidating every
// encrypted row, issued BFF client secret, or consent cookie signed with the old value) on
// every existing deployment's next boot.
const (
	hkdfInfoEncryptionKey = "docstore-oauth-at-rest-v1"
	hkdfInfoBFFSecret     = "docstore-bff-client-secret-v1"
	hkdfInfoConsentKey    = "docstore-consent-hmac-v1"
)

const (
	pemBlockType    = "EC PRIVATE KEY"
	kidRandomBytes  = 8
	masterKeyBytes  = 32
	derivedKeyBytes = 32
)

// KeyMaterial is the server's persistent cryptographic root, loaded from (or created in) the
// database on boot. Everything else — token signing, at-rest encryption, the first-party web
// client secret, consent-cookie signing — is the stored EC key or an HKDF derivation of the
// stored master secret, so a fresh database bootstraps itself and every replica sharing the
// database agrees on all derived values.
type KeyMaterial struct {
	Signer        *ecdsa.PrivateKey // ES256 access-token signing key
	KID           string            // stable JWK kid (random hex, generated once)
	EncryptionKey []byte            // 32 bytes — entstore at-rest encryption
	BFFSecret     string            // first-party web client secret (hex)
	ConsentKey    []byte            // 32 bytes — consent cookie HMAC
}

// LoadOrCreateKeyMaterial returns the persisted key material, creating and storing it on
// first boot. Concurrent first boots are resolved by the unique singleton row: the loser of
// the insert race re-reads the winner's row.
func LoadOrCreateKeyMaterial(ctx context.Context, c *ent.Client) (*KeyMaterial, error) {
	row, err := c.OAuthKey.Query().Only(ctx)
	switch {
	case err == nil:
		return keyMaterialFromRow(row)
	case !ent.IsNotFound(err):
		return nil, fmt.Errorf("oauthsrv: query key material: %w", err)
	}

	row, err = createKeyRow(ctx, c)
	if err != nil {
		if ent.IsConstraintError(err) {
			// Lost the create race to a concurrent first boot: the winner's row is
			// already committed, so read it back instead of erroring out.
			row, err = c.OAuthKey.Query().Only(ctx)
			if err != nil {
				return nil, fmt.Errorf("oauthsrv: load key material after create race: %w", err)
			}
			return keyMaterialFromRow(row)
		}
		return nil, fmt.Errorf("oauthsrv: create key material: %w", err)
	}
	return keyMaterialFromRow(row)
}

// createKeyRow generates fresh key material and inserts it as the singleton row.
func createKeyRow(ctx context.Context, c *ent.Client) (*ent.OAuthKey, error) {
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(signer)
	if err != nil {
		return nil, fmt.Errorf("marshal signing key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: pemBlockType, Bytes: der})

	kid, err := randomHex(kidRandomBytes)
	if err != nil {
		return nil, fmt.Errorf("generate kid: %w", err)
	}
	master, err := randomHex(masterKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("generate master secret: %w", err)
	}

	return c.OAuthKey.Create().
		SetSingleton(1).
		SetEcPrivateKeyPem(string(pemBytes)).
		SetKid(kid).
		SetMasterSecret(master).
		Save(ctx)
}

// keyMaterialFromRow parses a stored row into KeyMaterial, deriving the HKDF-based secrets
// from the decoded master secret.
func keyMaterialFromRow(row *ent.OAuthKey) (*KeyMaterial, error) {
	block, _ := pem.Decode([]byte(row.EcPrivateKeyPem))
	if block == nil {
		return nil, fmt.Errorf("oauthsrv: stored signing key is not valid PEM")
	}
	signer, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("oauthsrv: parse stored signing key: %w", err)
	}

	master, err := hex.DecodeString(row.MasterSecret)
	if err != nil {
		return nil, fmt.Errorf("oauthsrv: decode stored master secret: %w", err)
	}

	encryptionKey, err := deriveKey(master, hkdfInfoEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	bffSecretKey, err := deriveKey(master, hkdfInfoBFFSecret)
	if err != nil {
		return nil, fmt.Errorf("derive BFF secret: %w", err)
	}
	consentKey, err := deriveKey(master, hkdfInfoConsentKey)
	if err != nil {
		return nil, fmt.Errorf("derive consent key: %w", err)
	}

	return &KeyMaterial{
		Signer:        signer,
		KID:           row.Kid,
		EncryptionKey: encryptionKey,
		BFFSecret:     hex.EncodeToString(bffSecretKey),
		ConsentKey:    consentKey,
	}, nil
}

// deriveKey expands the master secret into derivedKeyBytes of key material for the given
// HKDF info string. The salt is nil because the master secret is already uniform-random
// (crypto/rand output), which HKDF's own RFC 5869 defines as an acceptable substitute for a
// random salt.
func deriveKey(master []byte, info string) ([]byte, error) {
	r := hkdf.New(sha256.New, master, nil, []byte(info))
	out := make([]byte, derivedKeyBytes)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// randomHex returns the hex encoding of n cryptographically random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
