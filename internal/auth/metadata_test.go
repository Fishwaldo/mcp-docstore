package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtectedResourceMetadata(t *testing.T) {
	h := ProtectedResourceMetadata("https://mcp.example.com", []string{"https://issuer.example.com"})
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		BearerMethods        []string `json:"bearer_methods_supported"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "https://mcp.example.com", body.Resource)
	require.Equal(t, []string{"https://issuer.example.com"}, body.AuthorizationServers)
	require.Contains(t, body.BearerMethods, "header")
}
