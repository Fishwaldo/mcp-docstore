package server_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/cmd/server"
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

func baseConfig(t *testing.T, issuer, listenAddr string) string {
	t.Helper()
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "idx.bleve")
	dbPath := filepath.Join(dir, "db.sqlite")
	listen := ""
	if listenAddr != "" {
		listen = "listen_addr: \"" + listenAddr + "\"\n"
	}
	return "public_url: \"https://docs.example.com\"\n" +
		listen +
		"bleve_index_path: \"" + idxPath + "\"\n" +
		"database: { driver: sqlite, dsn: \"file:" + dbPath + "?_pragma=foreign_keys(1)\" }\n" +
		"oidc: { issuer: \"" + issuer + "\", audience: \"mcp-docstore\" }\n" +
		"tenants:\n" +
		"  - { key: acme, name: Acme, match: { domains: [\"acme.com\"] } }\n"
}

func TestRunRebuildIndex(t *testing.T) {
	cfgPath := writeConfig(t, baseConfig(t, "https://idp.example.com", ""))
	err := server.Run(context.Background(), []string{"--config", cfgPath, "rebuild-index"}, discardLogger())
	require.NoError(t, err)
}

func TestRunUnknownSubcommand(t *testing.T) {
	cfgPath := writeConfig(t, baseConfig(t, "https://idp.example.com", ""))
	err := server.Run(context.Background(), []string{"--config", cfgPath, "bogus"}, discardLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bogus")
}

// startOIDC spins up an in-process OIDC server and returns its issuer URL.
func startOIDC(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: priv.Public(), KeyID: "test-key", Algorithm: oidc.RS256}},
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	srv.SetIssuer(ts.URL)
	return ts.URL
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestServeMetadataAndUnauthorized(t *testing.T) {
	issuer := startOIDC(t)
	addr := freeAddr(t)
	cfgPath := writeConfig(t, baseConfig(t, issuer, addr))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, []string{"--config", cfgPath, "serve"}, discardLogger())
	}()

	metaURL := "http://" + addr + "/.well-known/oauth-protected-resource"
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		resp, err = http.Get(metaURL)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metadata endpoint never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "https://docs.example.com")

	// Unauthenticated request to the MCP endpoint must be rejected with 401 + WWW-Authenticate.
	unauth, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	defer unauth.Body.Close()
	require.Equal(t, http.StatusUnauthorized, unauth.StatusCode)
	require.NotEmpty(t, unauth.Header.Get("WWW-Authenticate"))

	// The icon route is served unauthenticated as image/png.
	iconResp, err := http.Get("http://" + addr + "/icon-512.png")
	require.NoError(t, err)
	iconBody, err := io.ReadAll(iconResp.Body)
	require.NoError(t, err)
	require.NoError(t, iconResp.Body.Close())
	require.Equal(t, http.StatusOK, iconResp.StatusCode)
	require.Equal(t, "image/png", iconResp.Header.Get("Content-Type"))
	require.NotEmpty(t, iconBody)

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
