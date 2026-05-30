package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// dsnSafe makes a test name usable inside a sqlite file URI (subtest names can contain
// '/', '#', tabs, spaces, etc. which corrupt the URI).
var dsnUnsafe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func dsnSafe(name string) string { return dsnUnsafe.ReplaceAllString(name, "_") }

// fakeVerifier returns canned claims or an error, so the middleware can be tested
// without real OIDC.
type fakeVerifier struct {
	claims *Claims
	err    error
}

func (f fakeVerifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	return f.claims, f.err
}

func setup(t *testing.T, v Verifier) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.Open("sqlite", "file:auth-"+dsnSafe(t.Name())+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() { _ = s.Close() })
	// Seed the acme tenant (Phase 4 does this from config at boot).
	_, err = s.EnsureTenant(context.Background(), "acme", "Acme")
	require.NoError(t, err)

	resolver, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}},
	})
	require.NoError(t, err)

	// The inner handler records the identity it sees.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		require.True(t, ok)
		w.Header().Set("X-Tenant", id.TenantID.String())
		if len(id.Groups) > 0 {
			w.Header().Set("X-Groups", id.Groups[0])
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware(v, resolver, s, "https://srv/.well-known/oauth-protected-resource")
	return mw(inner), s
}

func do(t *testing.T, h http.Handler, authz string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddlewareMissingTokenIs401(t *testing.T) {
	h, _ := setup(t, fakeVerifier{})
	rec := do(t, h, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), `resource_metadata="https://srv/.well-known/oauth-protected-resource"`)
}

func TestMiddlewareInvalidTokenIs401(t *testing.T) {
	h, _ := setup(t, fakeVerifier{err: errors.New("bad sig")})
	rec := do(t, h, "Bearer xxx")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
}

func TestMiddlewareUnknownDomainIs403(t *testing.T) {
	h, _ := setup(t, fakeVerifier{claims: &Claims{Subject: "s", Email: "bob@unknown.org"}})
	rec := do(t, h, "Bearer ok")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestMiddlewareSuccessInjectsIdentity(t *testing.T) {
	h, _ := setup(t, fakeVerifier{claims: &Claims{Subject: "sub-1", Email: "alice@acme.com", Groups: []string{"eng"}}})
	rec := do(t, h, "Bearer ok")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, rec.Header().Get("X-Tenant"))
	require.Equal(t, "eng", rec.Header().Get("X-Groups"))
}

func TestMiddlewareCrossTenantRebindIs403(t *testing.T) {
	h, s := setup(t, fakeVerifier{claims: &Claims{Subject: "sub-x", Email: "alice@acme.com"}})
	// Pre-bind sub-x to a DIFFERENT tenant directly.
	other, err := s.EnsureTenant(context.Background(), "globex", "Globex")
	require.NoError(t, err)
	_, err = s.UpsertUser(context.Background(), other.ID, "sub-x", "x@globex.com")
	require.NoError(t, err)

	rec := do(t, h, "Bearer ok")
	require.Equal(t, http.StatusForbidden, rec.Code) // single-tenant binding violated → 403
}

func TestMiddlewareBearerParsing(t *testing.T) {
	// Only a well-formed "Bearer <non-empty>" header (scheme case-insensitive) should
	// reach the verifier; everything else is 401 without proceeding.
	cases := []struct {
		authz string
		ok    bool
	}{
		{"Bearer ok", true},
		{"bearer ok", true},
		{"BEARER ok", true},
		{"Bearer  ok", true}, // extra space collapses; token "ok"
		{"Bearer", false},
		{"Bearer ", false},
		{"Bearer    ", false}, // spaces only
		{"bearertoken", false},
		{"Basic abc", false},
		{"Bearer\tok", false}, // tab after scheme is not "Bearer "
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.authz, func(t *testing.T) {
			h, _ := setup(t, fakeVerifier{claims: &Claims{Subject: "sub-1", Email: "alice@acme.com"}})
			rec := do(t, h, tc.authz)
			if tc.ok {
				require.Equal(t, http.StatusOK, rec.Code)
			} else {
				require.Equal(t, http.StatusUnauthorized, rec.Code)
			}
		})
	}
}
