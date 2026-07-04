// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthconsent"
)

// signedConsentCookie builds a validly-signed ds_oauth_consent cookie value for entries,
// bypassing grantConsent's read-existing-then-merge behavior so tests can construct exact
// cookie contents (including already-expired entries).
func signedConsentCookie(t *testing.T, svc *Service, entries map[string]int64) string {
	t.Helper()
	canonical, err := json.Marshal(entries)
	require.NoError(t, err)
	mac := svc.consentMAC(canonical)
	payload, err := json.Marshal(consentCookieValue{V: 1, C: entries, M: hex.EncodeToString(mac)})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(payload)
}

func TestConsentCookie_GrantThenReadRoundTrips(t *testing.T) {
	svc := newMountTestService(t, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/consent", nil)
	require.NoError(t, svc.grantConsent(rec, req, thirdPartyClientID))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, consentCookieName, cookies[0].Name)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, consentCookiePath, cookies[0].Path)

	readReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	readReq.AddCookie(cookies[0])
	entries := svc.readConsentCookie(readReq)
	require.Contains(t, entries, thirdPartyClientID)
	require.True(t, entries[thirdPartyClientID] > time.Now().Unix())
}

func TestConsentCookie_TamperedPayloadRejected(t *testing.T) {
	svc := newMountTestService(t, false)

	valid := signedConsentCookie(t, svc, map[string]int64{thirdPartyClientID: time.Now().Add(time.Hour).Unix()})
	raw, err := base64.StdEncoding.DecodeString(valid)
	require.NoError(t, err)

	var v consentCookieValue
	require.NoError(t, json.Unmarshal(raw, &v))
	// Change the granted client_id's expiry without updating the HMAC — simulates an
	// attacker editing the readable (base64+JSON, not encrypted) cookie contents.
	v.C[thirdPartyClientID] = time.Now().Add(365 * 24 * time.Hour).Unix()
	tamperedPayload, err := json.Marshal(v)
	require.NoError(t, err)
	tampered := base64.StdEncoding.EncodeToString(tamperedPayload)

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	req.AddCookie(&http.Cookie{Name: consentCookieName, Value: tampered})

	entries := svc.readConsentCookie(req)
	require.Empty(t, entries, "a cookie whose HMAC does not match its contents must be treated as empty")
}

func TestConsentCookie_ExpiredEntryIsPruned(t *testing.T) {
	svc := newMountTestService(t, false)

	value := signedConsentCookie(t, svc, map[string]int64{thirdPartyClientID: time.Now().Add(-time.Minute).Unix()})
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	req.AddCookie(&http.Cookie{Name: consentCookieName, Value: value})

	entries := svc.readConsentCookie(req)
	require.NotContains(t, entries, thirdPartyClientID, "an entry past its expiry must not count as consent")
}

func TestConsentCookie_WrongHMACKeyRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	entries := map[string]int64{thirdPartyClientID: time.Now().Add(time.Hour).Unix()}
	canonical, err := json.Marshal(entries)
	require.NoError(t, err)

	// Sign with a different key entirely (not derived from svc.km.ConsentKey) to confirm
	// readConsentCookie is verifying the HMAC and not merely well-formedness of "m".
	mac := hmac.New(sha256.New, []byte("wrong-key-not-the-real-consent-key"))
	mac.Write(canonical)
	payload, err := json.Marshal(consentCookieValue{V: 1, C: entries, M: hex.EncodeToString(mac.Sum(nil))})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	req.AddCookie(&http.Cookie{Name: consentCookieName, Value: base64.StdEncoding.EncodeToString(payload)})

	require.Empty(t, svc.readConsentCookie(req))
}

func TestPruneOldestConsentEntries_CapsAtMax(t *testing.T) {
	entries := make(map[string]int64, 25)
	base := time.Now().Unix()
	for i := 0; i < 25; i++ {
		entries[string(rune('a'+i))] = base + int64(i) // later letters expire later
	}

	pruneOldestConsentEntries(entries, consentMaxEntries)

	require.Len(t, entries, consentMaxEntries)
	// The 5 earliest-expiring entries ("a".."e") should have been dropped, keeping the
	// newest consentMaxEntries.
	for i := 0; i < 5; i++ {
		require.NotContains(t, entries, string(rune('a'+i)))
	}
	for i := 5; i < 25; i++ {
		require.Contains(t, entries, string(rune('a'+i)))
	}
}

func TestResolveClientName_FallsBackToClientIDWhenUnknown(t *testing.T) {
	svc := newMountTestService(t, false)
	name := svc.resolveClientName(context.Background(), "no-such-client")
	require.Equal(t, "no-such-client", name)
}

func TestResolveClientName_UsesRegisteredName(t *testing.T) {
	svc := newMountTestService(t, false)
	name := svc.resolveClientName(context.Background(), thirdPartyClientID)
	require.Equal(t, thirdPartyClientName, name)
}

func TestCSRFToken_DifferentQueryRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	const cookie = "browser-csrf-cookie-value"
	token := svc.csrfToken(cookie, "client_id=a", dayBucket(time.Now()))
	require.False(t, svc.validCSRFToken(cookie, "client_id=b", token))
	require.True(t, svc.validCSRFToken(cookie, "client_id=a", token))
}

func TestCSRFToken_DifferentCookieRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	const query = "client_id=third-party-app"
	token := svc.csrfToken("cookie-A", query, dayBucket(time.Now()))
	require.False(t, svc.validCSRFToken("cookie-B", query, token), "a token minted against one browser's CSRF cookie must not validate against another's")
	require.True(t, svc.validCSRFToken("cookie-A", query, token))
}

func TestCSRFToken_YesterdayBucketAccepted(t *testing.T) {
	svc := newMountTestService(t, false)
	const cookie = "browser-csrf-cookie-value"
	const query = "client_id=third-party-app"
	// A token minted for the previous UTC day still validates, so a form rendered just before
	// a midnight-UTC rollover and submitted just after does not spuriously fail.
	token := svc.csrfToken(cookie, query, dayBucket(time.Now().Add(-24*time.Hour)))
	require.True(t, svc.validCSRFToken(cookie, query, token))
}

func TestCSRFToken_WrongDayBucketRejected(t *testing.T) {
	svc := newMountTestService(t, false)
	const cookie = "browser-csrf-cookie-value"
	query := "client_id=third-party-app"

	// Two days back is outside the accepted {today, yesterday} window.
	staleToken := svc.csrfToken(cookie, query, dayBucket(time.Now().Add(-48*time.Hour)))
	require.False(t, svc.validCSRFToken(cookie, query, staleToken), "a token minted for a day bucket outside the accepted window must not validate")
}

func TestRecordConsentAudit_WritesThenUpdatesRow(t *testing.T) {
	svc := newMountTestService(t, false)
	ctx := context.Background()

	require.NoError(t, svc.recordConsentAudit(ctx, thirdPartyClientID, thirdPartyClientName))

	row, err := svc.entc.OAuthConsent.Query().
		Where(oauthconsent.UserID(""), oauthconsent.ClientID(thirdPartyClientID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, thirdPartyClientName, row.ClientName)
	firstGrantedAt := row.GrantedAt

	// A second approval of the same client updates the existing row (refreshed name and
	// granted_at) rather than violating the (user_id, client_id) unique index.
	require.NoError(t, svc.recordConsentAudit(ctx, thirdPartyClientID, "Renamed Third Party App"))

	row2, err := svc.entc.OAuthConsent.Query().
		Where(oauthconsent.UserID(""), oauthconsent.ClientID(thirdPartyClientID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "Renamed Third Party App", row2.ClientName)
	require.True(t, !row2.GrantedAt.Before(firstGrantedAt))

	count, err := svc.entc.OAuthConsent.Query().Where(oauthconsent.ClientID(thirdPartyClientID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "a repeat approval must upsert, not insert a second row")
}
