// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package idptest implements an in-process fake upstream OIDC identity provider for tests.
// It serves discovery, JWKS, authorize, token, and userinfo endpoints over HTTPS at a
// "https://localhost:PORT" issuer URL — the hostname form that the dex generic-OIDC provider's
// SSRF guard accepts (it rejects loopback/private IP-literal issuers such as
// "https://127.0.0.1:PORT" unconditionally, regardless of AllowPrivateIP). This lets
// internal/oauthsrv's full authorization-server flow run end-to-end without a real IdP.
//
// It complements (does not replace) [github.com/coreos/go-oidc/v3/oidc/oidctest], which only
// serves discovery + JWKS: oauthsrv's dex provider additionally needs a working
// authorize/token/userinfo round trip to federate through.
package idptest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// User is the identity the fake IdP hands back from userinfo (and embeds in the signed
// id_token). It is exported and mutable on [Server.User] so a test can change the simulated
// identity between authorization-code flows without standing up a new server.
type User struct {
	Sub           string
	Email         string
	Name          string
	EmailVerified bool
	Groups        []string
}

// authRequest is what /authorize recorded for a still-live authorization code.
type authRequest struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	nonce               string
	expiresAt           time.Time
}

// Server is an in-process OIDC identity provider for tests: discovery, JWKS, authorize
// (auto-approves a fixed user), token, and userinfo. It lets the full authorization-server
// flow run end-to-end without a real IdP.
type Server struct {
	URL       string         // issuer, "https://localhost:PORT"
	User      User           // identity returned by userinfo (mutable between flows)
	RootCAs   *x509.CertPool // trusts this server's self-signed certificate
	CACertPEM []byte         // PEM encoding of the same certificate, for callers that need a file
	// (e.g. config.Load's oidc.root_ca) rather than an in-process *x509.CertPool.

	ts         *httptest.Server
	signingKey *rsa.PrivateKey
	kid        string

	mu            sync.Mutex
	codes         map[string]authRequest
	accessTokens  map[string]bool
	refreshTokens map[string]bool
}

// New starts a fake upstream OIDC provider for the lifetime of the test. The returned server is
// closed via t.Cleanup.
func New(t *testing.T) *Server {
	t.Helper()

	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("idptest: generate RSA signing key: %v", err)
	}
	kid, err := randomHex(8)
	if err != nil {
		t.Fatalf("idptest: generate kid: %v", err)
	}

	s := &Server{
		User: User{
			Sub:           "user-1",
			Email:         "user@example.com",
			Name:          "Test User",
			EmailVerified: true,
			Groups:        []string{"engineering"},
		},
		signingKey:    signingKey,
		kid:           kid,
		codes:         make(map[string]authRequest),
		accessTokens:  make(map[string]bool),
		refreshTokens: make(map[string]bool),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	mux.HandleFunc("/userinfo", s.handleUserinfo)
	mux.HandleFunc("/jwks", s.handleJWKS)

	tlsCert, leaf := generateLocalhostCert(t)
	ts := httptest.NewUnstartedServer(mux)
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("idptest: parse server URL: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	s.ts = ts
	s.URL = "https://localhost:" + u.Port()
	s.RootCAs = pool
	s.CACertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})

	return s
}

// generateLocalhostCert creates a fresh self-signed certificate covering DNS name "localhost"
// and the loopback IPs, so a test TLS server presenting it can be reached — and its hostname
// verified — via "https://localhost:PORT" rather than a loopback IP literal. This mirrors
// internal/oauthsrv's own oauthsrv_test.go helper of the same purpose; it is duplicated here
// rather than shared because idptest is a standalone package with its own test lifecycle.
func generateLocalhostCert(t *testing.T) (tls.Certificate, *x509.Certificate) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("idptest: generate cert key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("idptest: generate cert serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("idptest: create cert: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("idptest: parse cert: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, cert
}

// discoveryDoc is the subset of RFC 8414 / OpenID Connect Discovery 1.0 metadata that
// oauthsrv's dex-backed generic OIDC provider consumes.
type discoveryDoc struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	UserinfoEndpoint              string   `json:"userinfo_endpoint"`
	JWKSURI                       string   `json:"jwks_uri"`
	ScopesSupported               []string `json:"scopes_supported"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	SubjectTypesSupported         []string `json:"subject_types_supported"`
	IDTokenSigningAlgValues       []string `json:"id_token_signing_alg_values_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	doc := discoveryDoc{
		Issuer:                        s.URL,
		AuthorizationEndpoint:         s.URL + "/authorize",
		TokenEndpoint:                 s.URL + "/token",
		UserinfoEndpoint:              s.URL + "/userinfo",
		JWKSURI:                       s.URL + "/jwks",
		ScopesSupported:               []string{"openid", "profile", "email", "groups", "offline_access"},
		ResponseTypesSupported:        []string{"code"},
		SubjectTypesSupported:         []string{"public"},
		IDTokenSigningAlgValues:       []string{"RS256"},
		CodeChallengeMethodsSupported: []string{"S256"},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleAuthorize auto-approves every request as s.User: it records the PKCE challenge (if
// any) and redirects immediately with an issued code, with no login or consent UI.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "idptest: missing redirect_uri", http.StatusBadRequest)
		return
	}

	code, err := randomHex(16)
	if err != nil {
		http.Error(w, "idptest: generate code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.codes[code] = authRequest{
		clientID:            q.Get("client_id"),
		redirectURI:         redirectURI,
		codeChallenge:       q.Get("code_challenge"),
		codeChallengeMethod: q.Get("code_challenge_method"),
		nonce:               q.Get("nonce"),
		expiresAt:           time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()

	dest, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "idptest: invalid redirect_uri", http.StatusBadRequest)
		return
	}
	dq := dest.Query()
	dq.Set("code", code)
	if state := q.Get("state"); state != "" {
		dq.Set("state", state)
	}
	dest.RawQuery = dq.Encode()

	http.Redirect(w, r, dest.String(), http.StatusFound)
}

// handleToken validates the authorization code and, when PKCE was used, the code_verifier,
// then mints an access token, an RS256-signed id_token, and a refresh token. Any client_id and
// client_secret are accepted: idptest exists to exercise oauthsrv, not to be a hardened IdP.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "idptest: parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		http.Error(w, "idptest: unsupported grant_type", http.StatusBadRequest)
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")

	s.mu.Lock()
	req, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()

	if !ok || time.Now().After(req.expiresAt) {
		http.Error(w, "idptest: invalid or expired code", http.StatusBadRequest)
		return
	}

	if req.codeChallenge != "" {
		verifier := r.Form.Get("code_verifier")
		if verifier == "" || !pkceMatches(req.codeChallenge, req.codeChallengeMethod, verifier) {
			http.Error(w, "idptest: code_verifier does not match code_challenge", http.StatusBadRequest)
			return
		}
	}

	clientID := r.Form.Get("client_id")
	if clientID == "" {
		clientID = req.clientID
	}

	s.issueTokens(w, clientID, req.nonce)
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	token := r.Form.Get("refresh_token")

	s.mu.Lock()
	ok := s.refreshTokens[token]
	if ok {
		delete(s.refreshTokens, token)
	}
	s.mu.Unlock()

	if !ok {
		http.Error(w, "idptest: invalid refresh_token", http.StatusBadRequest)
		return
	}

	s.issueTokens(w, r.Form.Get("client_id"), "")
}

// pkceMatches verifies an RFC 7636 PKCE code_verifier against a stored code_challenge. Only
// the S256 method is supported (the discovery document advertises no other), and an unset
// method is treated as S256 since dex always sends it.
func pkceMatches(challenge, method, verifier string) bool {
	if method != "" && method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return computed == challenge
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

func (s *Server) issueTokens(w http.ResponseWriter, clientID, nonce string) {
	accessToken, err := randomHex(24)
	if err != nil {
		http.Error(w, "idptest: generate access_token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	refreshToken, err := randomHex(24)
	if err != nil {
		http.Error(w, "idptest: generate refresh_token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	idToken, err := s.signIDToken(clientID, nonce)
	if err != nil {
		http.Error(w, "idptest: sign id_token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.accessTokens[accessToken] = true
	s.refreshTokens[refreshToken] = true
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})
}

// signIDToken builds and RS256-signs an id_token for the current s.User.
func (s *Server) signIDToken(clientID, nonce string) (string, error) {
	now := time.Now()

	s.mu.Lock()
	user := s.User
	s.mu.Unlock()

	claims := map[string]any{
		"iss":            s.URL,
		"sub":            user.Sub,
		"aud":            clientID,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Unix(),
		"email":          user.Email,
		"email_verified": user.EmailVerified,
		"name":           user.Name,
		"groups":         user.Groups,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.signingKey}, &jose.SignerOptions{
		ExtraHeaders: map[jose.HeaderKey]any{"kid": s.kid},
	})
	if err != nil {
		return "", err
	}

	sig, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return sig.CompactSerialize()
}

func (s *Server) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	authz := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(authz, "Bearer ")
	if !ok || token == "" {
		http.Error(w, "idptest: missing bearer token", http.StatusUnauthorized)
		return
	}

	s.mu.Lock()
	valid := s.accessTokens[token]
	user := s.User
	s.mu.Unlock()

	if !valid {
		http.Error(w, "idptest: unknown access_token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":            user.Sub,
		"email":          user.Email,
		"email_verified": user.EmailVerified,
		"name":           user.Name,
		"groups":         user.Groups,
	})
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	set := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{{
			Key:       s.signingKey.Public(),
			KeyID:     s.kid,
			Algorithm: "RS256",
			Use:       "sig",
		}},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}

// randomHex returns the hex encoding of n cryptographically random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
