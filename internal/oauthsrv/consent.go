// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthconsent"
)

const (
	// consentCookieName is the cookie recording which client_ids a browser has already
	// approved, so the consent page is only shown once per client per cookie lifetime.
	consentCookieName = "ds_oauth_consent"
	// csrfCookieName holds a fresh per-browser random value set when the consent page is
	// rendered. The form's CSRF token is an HMAC bound to this value, so a token minted in
	// one browser (against that browser's cookie) cannot validate a POST from another
	// browser — this is what defeats the cross-site auto-POST that a query+day-only token
	// would allow. The cookie is HttpOnly, so page script cannot read it either.
	csrfCookieName = "ds_oauth_csrf"
	// consentCookiePath scopes the cookies to the authorization-server routes only — they
	// carry no session identity, so there is no reason to send them on every request.
	consentCookiePath = "/oauth/"
	// consentEntryTTL is how long a single client's approval is remembered.
	consentEntryTTL = 90 * 24 * time.Hour
	// csrfCookieTTL bounds how long the consent page may sit open before its form's CSRF
	// token stops validating.
	csrfCookieTTL = 30 * time.Minute
	// csrfRandomBytes is the size of the per-browser CSRF cookie's random value.
	csrfRandomBytes = 32
	// consentMaxEntries bounds the cookie's size: once exceeded, the entries with the
	// soonest expiry are dropped to make room for the newest approval.
	consentMaxEntries = 20
)

// consentFormMarker appears only in the consent page's HTML, so tests (and nothing else)
// can tell the consent page apart from every other response Mount can produce.
const consentFormMarker = `action="/oauth/consent"`

// consentCookieValue is the on-the-wire (base64+JSON) shape of the ds_oauth_consent cookie.
// C maps client_id to a Unix-seconds expiry; M is the hex HMAC-SHA256 of C's canonical JSON
// encoding (Go's encoding/json sorts map keys, so this is deterministic) keyed by
// KeyMaterial.ConsentKey. A cookie whose M does not match a freshly recomputed HMAC is
// rejected outright — it is never partially trusted.
type consentCookieValue struct {
	V int              `json:"v"`
	C map[string]int64 `json:"c"`
	M string           `json:"m"`
}

// consentPageData is what the consent page template renders. Every field here can be
// influenced by the requester (client_id, redirect_uri, the raw query string), so the
// template must rely on html/template's contextual auto-escaping — never template.HTML.
type consentPageData struct {
	ClientName     string
	RedirectURI    string
	AuthorizeQuery string
	CSRFToken      string
}

const consentPageHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Approve application</title>
<style>
body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1a1a1a;}
.card{border:1px solid #ccc;border-radius:8px;padding:1.5rem 2rem;}
.redirect{word-break:break-all;background:#f4f4f4;padding:0.5rem;border-radius:4px;}
button{font-size:1rem;padding:0.5rem 1.25rem;margin-top:1rem;margin-right:0.5rem;border-radius:4px;border:1px solid #888;cursor:pointer;}
button[value="approve"]{background:#1a73e8;color:#fff;border-color:#1a73e8;}
</style>
</head>
<body>
<div class="card">
<h1>Approve access</h1>
<p><strong>{{.ClientName}}</strong> wants to sign you in through this server's identity provider.</p>
<p>If approved, you will be redirected to:</p>
<p class="redirect">{{.RedirectURI}}</p>
<form method="POST" action="/oauth/consent">
<input type="hidden" name="authorize_query" value="{{.AuthorizeQuery}}">
<input type="hidden" name="csrf" value="{{.CSRFToken}}">
<button type="submit" name="decision" value="approve">Approve</button>
<button type="submit" name="decision" value="deny">Deny</button>
</form>
</div>
</body>
</html>
`

var consentTemplate = template.Must(template.New("consent").Parse(consentPageHTML))

// consentGate wraps inner (the library's registered OAuth routes) with the confused-deputy
// check described on Service: every /oauth/* request passes straight through to inner except
// GET /oauth/authorize, which is only forwarded once the requested client_id is either our
// first-party web SPA (docstore-web) or already covered by a valid consent cookie. Otherwise
// the consent page is served instead — inner never sees the request, so no upstream redirect
// can happen without this gate approving it first.
func (s *Service) consentGate(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/oauth/authorize" {
			inner.ServeHTTP(w, r)
			return
		}

		clientID := r.URL.Query().Get("client_id")
		if clientID == webClientID || s.hasConsent(r, clientID) {
			inner.ServeHTTP(w, r)
			return
		}

		s.serveConsentPage(w, r, clientID)
	})
}

// serveConsentPage renders the approval prompt for clientID. ClientName comes from the
// registered client when one exists, falling back to the raw client_id so an unregistered
// (but somehow reached) client_id still produces a legible page instead of a blank name.
func (s *Service) serveConsentPage(w http.ResponseWriter, r *http.Request, clientID string) {
	query := r.URL.RawQuery

	csrfCookie, err := randomCSRFCookieValue()
	if err != nil {
		s.logger.Error("oauthsrv: generate CSRF cookie", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfCookie,
		Path:     consentCookiePath,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(csrfCookieTTL.Seconds()),
	})

	data := consentPageData{
		ClientName:     s.resolveClientName(r.Context(), clientID),
		RedirectURI:    r.URL.Query().Get("redirect_uri"),
		AuthorizeQuery: query,
		CSRFToken:      s.csrfToken(csrfCookie, query, dayBucket(time.Now())),
	}

	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := consentTemplate.Execute(w, data); err != nil {
		s.logger.Error("oauthsrv: render consent page", "error", err)
	}
}

// randomCSRFCookieValue returns a fresh base64-encoded csrfRandomBytes value for the
// per-browser CSRF cookie.
func randomCSRFCookieValue() (string, error) {
	b := make([]byte, csrfRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// resolveClientName looks up clientID's registered display name, falling back to the raw
// client_id when the client is unknown (or the lookup fails for any other reason — a lookup
// error here must never block rendering the page, since the page IS the security control).
func (s *Service) resolveClientName(ctx context.Context, clientID string) string {
	client, err := s.srv.GetClient(ctx, clientID)
	if err != nil || client == nil || client.ClientName == "" {
		return clientID
	}
	return client.ClientName
}

// handleConsentSubmit serves POST /oauth/consent: the consent page's form target. A valid
// CSRF token plus decision=approve grants clientID (re-validated against the client store)
// consent and 303-redirects back to the original authorize request. Anything else — a bad
// CSRF token, an unknown client, or decision=deny/missing — never sets the cookie.
func (s *Service) handleConsentSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	// Reject a POST that a cross-origin page initiated. A present Origin that is not our own
	// origin is an unambiguous cross-site request; an absent Origin is left to the CSRF
	// cookie binding below, since some legitimate same-origin form posts omit the header.
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.serverOrigin() {
		http.Error(w, "cross-origin request rejected", http.StatusBadRequest)
		return
	}

	// The CSRF cookie is set per-browser when the consent page is rendered and is HttpOnly,
	// so an attacker cannot read a victim's value nor mint a token that binds to it. Without
	// the cookie there is nothing to bind the submitted token to, so the POST cannot be
	// trusted regardless of the token's value.
	csrfCookie, err := r.Cookie(csrfCookieName)
	if err != nil || csrfCookie.Value == "" {
		http.Error(w, "missing CSRF cookie", http.StatusBadRequest)
		return
	}

	query := r.FormValue("authorize_query")
	token := r.FormValue("csrf")
	if !s.validCSRFToken(csrfCookie.Value, query, token) {
		http.Error(w, "invalid or expired CSRF token", http.StatusBadRequest)
		return
	}

	if r.FormValue("decision") != "approve" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("access denied"))
		return
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		http.Error(w, "invalid authorize query", http.StatusBadRequest)
		return
	}
	clientID := values.Get("client_id")

	client, err := s.srv.GetClient(r.Context(), clientID)
	if err != nil || client == nil {
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	}

	if err := s.grantConsent(w, r, clientID); err != nil {
		s.logger.Error("oauthsrv: write consent cookie", "error", err, "client_id", clientID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.recordConsentAudit(r.Context(), clientID, client.ClientName); err != nil {
		// The cookie is already granted; a failed audit write should not block the login
		// the user just approved. Log and continue.
		s.logger.Error("oauthsrv: write consent audit row", "error", err, "client_id", clientID)
	}
	s.logger.Info("oauth consent granted", "client_id", clientID)

	http.Redirect(w, r, "/oauth/authorize?"+query, http.StatusSeeOther)
}

// hasConsent reports whether r's consent cookie currently covers clientID.
func (s *Service) hasConsent(r *http.Request, clientID string) bool {
	entries := s.readConsentCookie(r)
	_, ok := entries[clientID]
	return ok
}

// readConsentCookie parses and verifies the ds_oauth_consent cookie, returning the map of
// still-valid client_id -> expiry entries. Any failure — missing cookie, malformed JSON, or
// (critically) an HMAC that does not match — yields an empty map, i.e. "no consent granted".
// A tampered cookie is never partially honored.
func (s *Service) readConsentCookie(r *http.Request) map[string]int64 {
	c, err := r.Cookie(consentCookieName)
	if err != nil || c.Value == "" {
		return nil
	}

	raw, err := base64.StdEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}

	var v consentCookieValue
	if err := json.Unmarshal(raw, &v); err != nil || v.V != 1 || v.C == nil {
		return nil
	}

	canonical, err := json.Marshal(v.C)
	if err != nil {
		return nil
	}
	got, err := hex.DecodeString(v.M)
	if err != nil || !hmac.Equal(got, s.consentMAC(canonical)) {
		return nil
	}

	now := time.Now().Unix()
	valid := make(map[string]int64, len(v.C))
	for id, expiry := range v.C {
		if expiry > now {
			valid[id] = expiry
		}
	}
	return valid
}

// grantConsent adds clientID (with a fresh consentEntryTTL expiry) to the caller's existing
// valid consent entries and writes the re-signed cookie.
func (s *Service) grantConsent(w http.ResponseWriter, r *http.Request, clientID string) error {
	entries := s.readConsentCookie(r)
	if entries == nil {
		entries = make(map[string]int64)
	}
	entries[clientID] = time.Now().Add(consentEntryTTL).Unix()
	pruneOldestConsentEntries(entries, consentMaxEntries)

	canonical, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	mac := s.consentMAC(canonical)

	payload, err := json.Marshal(consentCookieValue{V: 1, C: entries, M: hex.EncodeToString(mac)})
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     consentCookieName,
		Value:    base64.StdEncoding.EncodeToString(payload),
		Path:     consentCookiePath,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(consentEntryTTL.Seconds()),
	})
	return nil
}

// pruneOldestConsentEntries removes the entries with the soonest expiry until at most max
// remain, so a cookie cannot grow without bound across many distinct clients.
func pruneOldestConsentEntries(entries map[string]int64, max int) {
	for len(entries) > max {
		var oldestID string
		var oldestExpiry int64
		first := true
		for id, expiry := range entries {
			if first || expiry < oldestExpiry {
				oldestID, oldestExpiry, first = id, expiry, false
			}
		}
		delete(entries, oldestID)
	}
}

// consentMAC computes HMAC-SHA256(ConsentKey, canonical) — the primitive shared by the
// cookie's integrity check and (with a different message) the CSRF token below.
func (s *Service) consentMAC(canonical []byte) []byte {
	mac := hmac.New(sha256.New, s.km.ConsentKey)
	mac.Write(canonical)
	return mac.Sum(nil)
}

// csrfToken derives the consent form's CSRF token by HMAC-ing the per-browser CSRF cookie
// value together with the authorize query string and a UTC day bucket. Binding the cookie
// value in is what makes the token per-browser: a token minted against one browser's cookie
// cannot validate a POST carrying a different browser's cookie, so an attacker's token
// (minted against their own cookie) is worthless in a victim's browser. Requiring no
// server-side storage, it validates by recomputation alone.
func (s *Service) csrfToken(csrfCookie, query, bucket string) string {
	mac := hmac.New(sha256.New, s.km.ConsentKey)
	mac.Write([]byte(csrfCookie + "|" + query + "|" + bucket))
	return hex.EncodeToString(mac.Sum(nil))
}

// validCSRFToken reports whether token is a valid CSRF token for csrfCookie+query right now,
// comparing in constant time. Both the current UTC day and the previous one are accepted so a
// form rendered just before a midnight-UTC boundary and submitted just after does not
// spuriously fail; the per-browser cookie binding is what actually gates the request, so the
// slightly wider time window costs nothing.
func (s *Service) validCSRFToken(csrfCookie, query, token string) bool {
	got, err := hex.DecodeString(token)
	if err != nil {
		return false
	}
	now := time.Now()
	for _, bucket := range []string{dayBucket(now), dayBucket(now.Add(-24 * time.Hour))} {
		want, err := hex.DecodeString(s.csrfToken(csrfCookie, query, bucket))
		if err != nil {
			continue
		}
		if hmac.Equal(got, want) {
			return true
		}
	}
	return false
}

// dayBucket returns t's UTC calendar day as "YYYY-MM-DD", the coarse time window CSRF tokens
// are scoped to.
func dayBucket(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// serverOrigin is the scheme+host of the configured PublicURL — the only Origin a legitimate
// same-origin consent POST can carry. A parse failure yields "", which never equals a
// present Origin header, so the cross-origin check fails closed.
func (s *Service) serverOrigin() string {
	u, err := url.Parse(s.cfg.PublicURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// recordConsentAudit upserts the audit row for clientID: user_id is always "" because
// consent happens before the browser has completed the upstream login that would resolve an
// identity (see the OAuthConsent schema doc). A second approval of the same client updates
// client_name and granted_at on the existing row rather than inserting a duplicate.
func (s *Service) recordConsentAudit(ctx context.Context, clientID, clientName string) error {
	existing, err := s.entc.OAuthConsent.Query().
		Where(oauthconsent.UserID(""), oauthconsent.ClientID(clientID)).
		Only(ctx)
	if err == nil {
		_, uerr := existing.Update().SetClientName(clientName).SetGrantedAt(time.Now()).Save(ctx)
		return uerr
	}
	if !ent.IsNotFound(err) {
		return err
	}

	_, err = s.entc.OAuthConsent.Create().
		SetUserID("").
		SetClientID(clientID).
		SetClientName(clientName).
		Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsConstraintError(err) {
		return err
	}

	// Lost a concurrent-approval race: the row now exists. Re-query and reconcile it,
	// mirroring internal/store.Store.UpsertUser's pattern for the same race.
	existing, qerr := s.entc.OAuthConsent.Query().
		Where(oauthconsent.UserID(""), oauthconsent.ClientID(clientID)).
		Only(ctx)
	if qerr != nil {
		return err
	}
	_, uerr := existing.Update().SetClientName(clientName).SetGrantedAt(time.Now()).Save(ctx)
	return uerr
}
