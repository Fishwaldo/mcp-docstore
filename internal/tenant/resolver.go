// Package tenant resolves authenticated email identities to a configured tenant key.
package tenant

import (
	"strings"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
)

// Resolver maps emails/domains to tenant keys. Built once from config; read-only.
type Resolver struct {
	byEmail  map[string]string
	byDomain map[string]string
}

func NewResolver(specs []config.TenantSpec) (*Resolver, error) {
	r := &Resolver{byEmail: map[string]string{}, byDomain: map[string]string{}}
	for _, s := range specs {
		for _, e := range s.Match.Emails {
			r.byEmail[strings.ToLower(e)] = s.Key
		}
		for _, d := range s.Match.Domains {
			r.byDomain[strings.ToLower(d)] = s.Key
		}
	}
	return r, nil
}

// Resolve returns the tenant key for an email, exact-match first then domain.
// Returns ok=false for malformed emails or unmapped identities.
func (r *Resolver) Resolve(email string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 {
		return "", false // malformed
	}
	if key, ok := r.byEmail[e]; ok {
		return key, true
	}
	domain := e[at+1:]
	if key, ok := r.byDomain[domain]; ok {
		return key, true
	}
	return "", false
}

// ValidEmail reports whether s is a structurally acceptable email address.
func ValidEmail(s string) bool {
	e := strings.TrimSpace(s)
	at := strings.LastIndex(e, "@")
	return at > 0 && at < len(e)-1
}
