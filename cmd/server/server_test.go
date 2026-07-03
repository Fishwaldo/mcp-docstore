// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/cmd/server"
	"github.com/Fishwaldo/mcp-docstore/internal/oauthsrv/idptest"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeConfig writes a config YAML to a temp file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// rootCAFile writes pemBytes (the upstream idptest server's certificate) to a temp file and
// returns its path, for use as oidc.root_ca — config.Load only accepts a file path, unlike the
// in-process *x509.CertPool other packages' tests wire directly into oauthsrv.Config.
func rootCAFile(t *testing.T, pemBytes []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return path
}

// baseConfig returns a minimal config YAML wiring the embedded OAuth authorization server to
// federate login to idp. dbPath, when non-empty, pins the sqlite DSN to a real file (rather
// than a fresh temp one) so a second Run against the same path reuses the persisted key
// material.
func baseConfig(t *testing.T, idp *idptest.Server, listenAddr, dbPath string) string {
	t.Helper()
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.bleve")
	if dbPath == "" {
		dbPath = filepath.Join(dir, "db.sqlite")
	}
	listen := ""
	if listenAddr != "" {
		listen = "listen_addr: \"" + listenAddr + "\"\n"
	}
	caPath := rootCAFile(t, idp.CACertPEM)
	return "public_url: \"https://docs.example.com\"\n" +
		listen +
		"bleve_index_path: \"" + idxPath + "\"\n" +
		"database: { driver: sqlite, dsn: \"file:" + dbPath + "?_pragma=foreign_keys(1)\" }\n" +
		"oidc:\n" +
		"  issuer: \"" + idp.URL + "\"\n" +
		"  client_id: \"upstream-client\"\n" +
		"  client_secret: \"upstream-secret\"\n" +
		"  allow_private_ip: true\n" +
		"  root_ca: \"" + caPath + "\"\n" +
		"tenants:\n" +
		"  - { key: acme, name: Acme, match: { domains: [\"acme.com\"] } }\n"
}

// webConfig appends a web: block enabling the BFF. The BFF is a first-party client of our own
// embedded authorization server (auto-seeded on boot), so unlike before it needs no OAuth
// client credentials or redirect URL of its own.
func webConfig(base string) string {
	return base +
		"web:\n" +
		"  cookie_secure: false\n"
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// waitReady polls the AS metadata endpoint (always mounted, web or not) until it responds.
func waitReady(t *testing.T, addr string) {
	t.Helper()
	metaURL := "http://" + addr + "/.well-known/oauth-authorization-server"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(metaURL)
		if err == nil {
			require.NoError(t, resp.Body.Close())
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runServer boots server.Run in the background and returns a stop func that cancels it and
// blocks until Run has returned (failing the test if Run returned a non-nil error).
func runServer(t *testing.T, cfgPath string) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()
	return func() {
		cancel()
		select {
		case err := <-runErr:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after context cancel")
		}
	}
}

func TestRunRebuildIndex(t *testing.T) {
	idp := idptest.New(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, "", ""))
	err := server.Run(context.Background(), []string{"--config", cfgPath, "rebuild-index"}, discardLogger())
	require.NoError(t, err)
}

func TestRunUnknownSubcommand(t *testing.T) {
	idp := idptest.New(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, "", ""))
	err := server.Run(context.Background(), []string{"--config", cfgPath, "bogus"}, discardLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bogus")
}

func TestRunGracefulShutdown(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, addr, ""))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()

	waitReady(t, addr)

	cancel() // simulate SIGTERM/SIGINT delivered to the signal context
	select {
	case err := <-runErr:
		require.NoError(t, err, "Run must return nil on graceful shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// TestASMetadataAndMCPUnauthorized boots with web enabled and asserts the embedded
// authorization server's discovery documents are live and that /mcp still demands a bearer
// token whose challenge points back at the protected-resource metadata.
func TestASMetadataAndMCPUnauthorized(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, webConfig(baseConfig(t, idp, addr, "")))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()
	waitReady(t, addr)

	asResp, err := http.Get("http://" + addr + "/.well-known/oauth-authorization-server")
	require.NoError(t, err)
	asBody, err := io.ReadAll(asResp.Body)
	require.NoError(t, err)
	require.NoError(t, asResp.Body.Close())
	require.Equal(t, http.StatusOK, asResp.StatusCode)
	var asMeta struct {
		Issuer string `json:"issuer"`
	}
	require.NoError(t, json.Unmarshal(asBody, &asMeta))
	require.Equal(t, "https://docs.example.com", asMeta.Issuer)

	prmResp, err := http.Get("http://" + addr + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	prmBody, err := io.ReadAll(prmResp.Body)
	require.NoError(t, err)
	require.NoError(t, prmResp.Body.Close())
	require.Equal(t, http.StatusOK, prmResp.StatusCode)
	var prmMeta struct {
		Resource string `json:"resource"`
	}
	require.NoError(t, json.Unmarshal(prmBody, &prmMeta))
	require.Equal(t, "https://docs.example.com/mcp", prmMeta.Resource)

	mcpResp, err := http.Post("http://"+addr+"/mcp", "application/json", nil)
	require.NoError(t, err)
	require.NoError(t, mcpResp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, mcpResp.StatusCode)
	require.Contains(t, mcpResp.Header.Get("WWW-Authenticate"), "resource_metadata")

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// TestMCPGarbageBearerUnauthorized asserts a syntactically-present-but-invalid bearer token is
// rejected the same way an absent one is, and that the discovery endpoints stay up regardless.
func TestMCPGarbageBearerUnauthorized(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, addr, ""))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()
	waitReady(t, addr)

	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource",
		"/.well-known/jwks.json",
	} {
		metaResp, err := http.Get("http://" + addr + path)
		require.NoError(t, err)
		require.NoError(t, metaResp.Body.Close())
		require.Equal(t, http.StatusOK, metaResp.StatusCode, path)
	}

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// TestWebDisabledASAlwaysOn asserts that with no web: block the embedded authorization server
// still routes /oauth/authorize (it is never conditional on the web UI), while the BFF's own
// /auth/login route is absent (404).
func TestWebDisabledASAlwaysOn(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, baseConfig(t, idp, addr, ""))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()
	waitReady(t, addr)

	authorizeResp, err := http.Get("http://" + addr + "/oauth/authorize")
	require.NoError(t, err)
	require.NoError(t, authorizeResp.Body.Close())
	require.NotEqual(t, http.StatusNotFound, authorizeResp.StatusCode, "/oauth/authorize must route even when web: is absent")

	loginResp, err := http.Get("http://" + addr + "/auth/login")
	require.NoError(t, err)
	require.NoError(t, loginResp.Body.Close())
	require.Equal(t, http.StatusNotFound, loginResp.StatusCode, "the BFF must not be mounted when web: is absent")

	// / is not the MCP handler either; ServeMux returns 404.
	rootResp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	require.NoError(t, rootResp.Body.Close())
	require.Equal(t, http.StatusNotFound, rootResp.StatusCode)

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// jwksKID fetches /.well-known/jwks.json and returns the sole key's kid.
func jwksKID(t *testing.T, addr string) string {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/.well-known/jwks.json")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(body, &set))
	require.Len(t, set.Keys, 1)
	require.NotEmpty(t, set.Keys[0].Kid)
	return set.Keys[0].Kid
}

// TestKeyMaterialStableAcrossReboots boots the server twice against the same on-disk database
// and asserts the second boot loads (rather than regenerates) the persisted signing key: the
// JWKS kid is identical across both Run-level constructions.
func TestKeyMaterialStableAcrossReboots(t *testing.T) {
	idp := idptest.New(t)
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")

	addr1 := freeAddr(t)
	cfgPath1 := writeConfig(t, baseConfig(t, idp, addr1, dbPath))
	stop1 := runServer(t, cfgPath1)
	waitReady(t, addr1)
	kid1 := jwksKID(t, addr1)
	stop1()

	addr2 := freeAddr(t)
	cfgPath2 := writeConfig(t, baseConfig(t, idp, addr2, dbPath))
	stop2 := runServer(t, cfgPath2)
	waitReady(t, addr2)
	kid2 := jwksKID(t, addr2)
	stop2()

	require.Equal(t, kid1, kid2, "the signing key (and its kid) must be reused across boots on the same database")
}

// TestWebEnabledModeRoutes verifies that when web: is present the server mounts /auth/login
// (redirect to the embedded AS's own /oauth/authorize) and /api/projects (session-gated 401) in
// addition to /mcp.
func TestWebEnabledModeRoutes(t *testing.T) {
	idp := idptest.New(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, webConfig(baseConfig(t, idp, addr, "")))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()
	waitReady(t, addr)

	// /mcp still requires a bearer token.
	mcpResp, err := http.Post("http://"+addr+"/mcp", "application/json", nil)
	require.NoError(t, err)
	require.NoError(t, mcpResp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, mcpResp.StatusCode)

	// /auth/login redirects to our own AS's /oauth/authorize (302).
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginResp, err := client.Get("http://" + addr + "/auth/login")
	require.NoError(t, err)
	require.NoError(t, loginResp.Body.Close())
	require.Equal(t, http.StatusFound, loginResp.StatusCode)

	// /api/projects without a session returns 401.
	apiResp, err := http.Get("http://" + addr + "/api/projects")
	require.NoError(t, err)
	require.NoError(t, apiResp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, apiResp.StatusCode)

	// / serves the SPA (200 with HTML).
	rootResp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	rootBody, err := io.ReadAll(rootResp.Body)
	require.NoError(t, err)
	require.NoError(t, rootResp.Body.Close())
	require.Equal(t, http.StatusOK, rootResp.StatusCode)
	require.Contains(t, rootResp.Header.Get("Content-Type"), "text/html")
	require.NotEmpty(t, rootBody)

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
