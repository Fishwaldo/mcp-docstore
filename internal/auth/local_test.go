// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	josejose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/giantswarm/mcp-oauth/storage"
)

const (
	testIssuer   = "https://docstore.example.com"
	testAudience = "https://docstore.example.com/mcp"
)

// fakeRevocationChecker is a stub RevocationChecker for tests.
type fakeRevocationChecker struct {
	revokedJTIs   map[string]bool
	families      map[string]*storage.RefreshTokenFamilyMetadata
	familyLookErr error // returned (non-nil) for any family lookup not found in families
}

func newFakeRevocationChecker() *fakeRevocationChecker {
	return &fakeRevocationChecker{
		revokedJTIs: map[string]bool{},
		families:    map[string]*storage.RefreshTokenFamilyMetadata{},
	}
}

func (f *fakeRevocationChecker) IsJTIRevoked(ctx context.Context, jti string) (bool, error) {
	return f.revokedJTIs[jti], nil
}

func (f *fakeRevocationChecker) GetRefreshTokenFamilyByID(ctx context.Context, familyID string) (*storage.RefreshTokenFamilyMetadata, error) {
	if fam, ok := f.families[familyID]; ok {
		return fam, nil
	}
	if f.familyLookErr != nil {
		return nil, f.familyLookErr
	}
	return nil, storage.ErrRefreshTokenFamilyNotFound
}

// testTokenClaims mirrors the claim shape the AS issues in a self-signed access token.
type testTokenClaims struct {
	Issuer        string   `json:"iss"`
	Subject       string   `json:"sub"`
	Audience      []string `json:"aud"`
	Expiry        int64    `json:"exp"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Groups        []string `json:"groups"`
	JTI           string   `json:"jti,omitempty"`
	FamilyID      string   `json:"family_id,omitempty"`
}

func mintToken(t *testing.T, key crypto.Signer, alg josejose.SignatureAlgorithm, claims testTokenClaims) string {
	t.Helper()
	signer, err := josejose.NewSigner(josejose.SigningKey{Algorithm: alg, Key: key}, nil)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	tok, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("serialize token: %v", err)
	}
	return tok
}

func defaultClaims() testTokenClaims {
	return testTokenClaims{
		Issuer:        testIssuer,
		Subject:       "user-123",
		Audience:      []string{testAudience},
		Expiry:        time.Now().Add(time.Hour).Unix(),
		Email:         "user@example.com",
		EmailVerified: true,
		Groups:        []string{"engineering", "on-call"},
	}
}

func TestLocalVerifierValidToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	tok := mintToken(t, key, josejose.ES256, defaultClaims())
	claims, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject = %q, want user-123", claims.Subject)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("Email = %q, want user@example.com", claims.Email)
	}
	if len(claims.Groups) != 2 || claims.Groups[0] != "engineering" || claims.Groups[1] != "on-call" {
		t.Errorf("Groups = %v, want [engineering on-call]", claims.Groups)
	}
	if claims.EmailVerified == nil || !*claims.EmailVerified {
		t.Errorf("EmailVerified = %v, want true", claims.EmailVerified)
	}
}

func TestLocalVerifierWrongIssuer(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Issuer = "https://not-us.example.com"
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for wrong issuer")
	}
}

func TestLocalVerifierWrongAudience(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Audience = []string{"https://someone-else.example.com/mcp"}
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for wrong audience")
	}
}

func TestLocalVerifierExpired(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Expiry = time.Now().Add(-time.Hour).Unix()
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for expired token")
	}
}

func TestLocalVerifierWrongAlgorithm(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	// The verifier is only ever configured with the AS's own ES256 public key, so an
	// RS256-signed token (even naming the right issuer/audience/subject) must be rejected
	// on algorithm grounds — it could not have come from our signer.
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{ecKey.Public()}, rc)

	tok := mintToken(t, rsaKey, josejose.RS256, defaultClaims())
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for wrong signing algorithm")
	}
}

func TestLocalVerifierRevokedJTI(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	rc.revokedJTIs["jti-1"] = true
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.JTI = "jti-1"
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for revoked jti")
	}
}

func TestLocalVerifierRevokedFamily(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	rc.families["family-1"] = &storage.RefreshTokenFamilyMetadata{FamilyID: "family-1", Revoked: true}
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.FamilyID = "family-1"
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error for revoked family")
	}
}

func TestLocalVerifierUnknownFamilyIsOK(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker() // no families registered -> ErrRefreshTokenFamilyNotFound
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.FamilyID = "unknown-family"
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("Verify() error = %v, want nil for unknown family", err)
	}
}

func TestLocalVerifierTrailingSlashAudience(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Audience = []string{testAudience + "/"}
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("Verify() error = %v, want nil for trailing-slash audience", err)
	}
}

func TestLocalVerifierFamilyStorageErrorFailsClosed(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rc := newFakeRevocationChecker()
	rc.familyLookErr = errors.New("storage unavailable")
	v := NewLocalVerifier(testIssuer, testAudience, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.FamilyID = "family-x"
	tok := mintToken(t, key, josejose.ES256, claims)

	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("Verify() error = nil, want error when family storage lookup fails")
	}
}
