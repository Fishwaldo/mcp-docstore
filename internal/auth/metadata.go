package auth

import (
	"encoding/json"
	"net/http"
)

// ProtectedResourceMetadata returns an http.Handler serving RFC 9728 protected-resource
// metadata: the resource identifier and the authorization server(s) clients should use.
func ProtectedResourceMetadata(resource string, authorizationServers []string) http.Handler {
	body := map[string]any{
		"resource":                 resource,
		"authorization_servers":    authorizationServers,
		"bearer_methods_supported": []string{"header"},
	}
	payload, _ := json.Marshal(body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
}
