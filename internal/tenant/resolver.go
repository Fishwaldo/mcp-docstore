// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package tenant resolves authenticated email identities to a configured tenant key.
package tenant

import (
	"fmt"
	"strings"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
)

// Resolver maps emails/domains to tenant keys. Built once from config; read-only.
type Resolver struct {
	byEmail  map[string]string
	byDomain map[string]string
	admins   map[string]map[string]bool
}

// NewResolver builds a resolver from tenant specs. It fails closed on a duplicate email
// or domain mapped to two tenants (defense-in-depth: config.Validate already enforces
// this, but NewResolver must not silently last-write-wins if called directly).
func NewResolver(specs []config.TenantSpec) (*Resolver, error) {
	r := &Resolver{byEmail: map[string]string{}, byDomain: map[string]string{}, admins: map[string]map[string]bool{}}
	for _, s := range specs {
		if len(s.Admins) > 0 {
			set := r.admins[s.Key]
			if set == nil {
				set = map[string]bool{}
				r.admins[s.Key] = set
			}
			for _, a := range s.Admins {
				set[strings.ToLower(strings.TrimSpace(a))] = true
			}
		}
		for _, e := range s.Match.Emails {
			e = strings.ToLower(e)
			if other, dup := r.byEmail[e]; dup && other != s.Key {
				return nil, fmt.Errorf("email %q mapped to both %q and %q", e, other, s.Key)
			}
			r.byEmail[e] = s.Key
		}
		for _, d := range s.Match.Domains {
			d = strings.ToLower(d)
			if other, dup := r.byDomain[d]; dup && other != s.Key {
				return nil, fmt.Errorf("domain %q mapped to both %q and %q", d, other, s.Key)
			}
			r.byDomain[d] = s.Key
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

// IsAdmin reports whether email is a configured admin of the tenant identified by key.
func (r *Resolver) IsAdmin(key, email string) bool {
	set, ok := r.admins[key]
	if !ok {
		return false
	}
	return set[strings.ToLower(strings.TrimSpace(email))]
}

// ValidEmail reports whether s is a structurally acceptable email address.
func ValidEmail(s string) bool {
	e := strings.TrimSpace(s)
	at := strings.LastIndex(e, "@")
	return at > 0 && at < len(e)-1
}
