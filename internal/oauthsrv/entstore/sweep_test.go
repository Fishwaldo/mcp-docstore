// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthcode"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthstate"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthprovidertoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshfamily"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrevokedjti"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthtokenmetadata"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthuserinfo"
)

// TestDeleteExpired seeds one expired and one live row in every table DeleteExpired sweeps,
// plus a revoked-family retention case, and asserts only the expired/aged-out rows are
// removed and the returned count matches exactly.
func TestDeleteExpired(t *testing.T) {
	store, client := newTestEntStore(t)
	ctx := context.Background()

	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	// OAuthAuthState
	require.NoError(t, client.OAuthAuthState.Create().
		SetStateID("state-expired").SetClientID("c").SetRedirectURI("https://x/cb").
		SetProviderState("ps-expired").SetProviderCodeVerifier("v").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthAuthState.Create().
		SetStateID("state-live").SetClientID("c").SetRedirectURI("https://x/cb").
		SetProviderState("ps-live").SetProviderCodeVerifier("v").SetExpiresAt(future).Exec(ctx))

	// OAuthAuthCode
	require.NoError(t, client.OAuthAuthCode.Create().
		SetCode("code-expired").SetClientID("c").SetRedirectURI("https://x/cb").
		SetUserID("u").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthAuthCode.Create().
		SetCode("code-live").SetClientID("c").SetRedirectURI("https://x/cb").
		SetUserID("u").SetExpiresAt(future).Exec(ctx))

	// OAuthProviderToken
	require.NoError(t, client.OAuthProviderToken.Create().
		SetUserID("user-pt-expired").SetTokenJSON("blob").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthProviderToken.Create().
		SetUserID("user-pt-live").SetTokenJSON("blob").SetExpiresAt(future).Exec(ctx))

	// OAuthUserInfo
	require.NoError(t, client.OAuthUserInfo.Create().
		SetUserID("user-ui-expired").SetInfoJSON("blob").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthUserInfo.Create().
		SetUserID("user-ui-live").SetInfoJSON("blob").SetExpiresAt(future).Exec(ctx))

	// OAuthRefreshToken
	require.NoError(t, client.OAuthRefreshToken.Create().
		SetTokenHash("hash-rt-expired").SetUserID("u").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthRefreshToken.Create().
		SetTokenHash("hash-rt-live").SetUserID("u").SetExpiresAt(future).Exec(ctx))

	// OAuthRevokedJTI
	require.NoError(t, client.OAuthRevokedJTI.Create().
		SetJti("jti-expired").SetExpiresAt(past).Exec(ctx))
	require.NoError(t, client.OAuthRevokedJTI.Create().
		SetJti("jti-live").SetExpiresAt(future).Exec(ctx))

	// OAuthTokenMetadata: expired, live, and one with no expiry set (must survive untouched
	// since expires_at is optional and a zero value means "unknown", not "expired").
	require.NoError(t, client.OAuthTokenMetadata.Create().
		SetTokenID("meta-expired").SetUserID("u").SetClientID("c").
		SetIssuedAt(past).SetExpiresAt(past).SetTokenType("access").SetScopes([]string{}).Exec(ctx))
	require.NoError(t, client.OAuthTokenMetadata.Create().
		SetTokenID("meta-live").SetUserID("u").SetClientID("c").
		SetIssuedAt(now).SetExpiresAt(future).SetTokenType("access").SetScopes([]string{}).Exec(ctx))
	require.NoError(t, client.OAuthTokenMetadata.Create().
		SetTokenID("meta-noexpiry").SetUserID("u").SetClientID("c").
		SetIssuedAt(now).SetTokenType("refresh").SetScopes([]string{}).Exec(ctx))

	// OAuthRefreshFamily: revoked 31 days ago (swept), revoked 1 hour ago (kept), never
	// revoked (kept regardless of age).
	require.NoError(t, client.OAuthRefreshFamily.Create().
		SetFamilyID("family-old-revoked").SetUserID("u").SetClientID("c").SetGeneration(1).
		SetIssuedAt(now.Add(-60*24*time.Hour)).SetRevoked(true).
		SetRevokedAt(now.Add(-31*24*time.Hour)).Exec(ctx))
	require.NoError(t, client.OAuthRefreshFamily.Create().
		SetFamilyID("family-recent-revoked").SetUserID("u").SetClientID("c").SetGeneration(1).
		SetIssuedAt(now.Add(-time.Hour)).SetRevoked(true).
		SetRevokedAt(now.Add(-time.Hour)).Exec(ctx))
	require.NoError(t, client.OAuthRefreshFamily.Create().
		SetFamilyID("family-active").SetUserID("u").SetClientID("c").SetGeneration(1).
		SetIssuedAt(now.Add(-60*24*time.Hour)).Exec(ctx))

	n, err := store.DeleteExpired(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 8, n, "7 expired rows across the swept tables + 1 aged-out revoked family")

	// Expired/aged-out rows are gone.
	require.False(t, client.OAuthAuthState.Query().Where(oauthauthstate.StateID("state-expired")).ExistX(ctx))
	require.False(t, client.OAuthAuthCode.Query().Where(oauthauthcode.Code("code-expired")).ExistX(ctx))
	require.False(t, client.OAuthProviderToken.Query().Where(oauthprovidertoken.UserID("user-pt-expired")).ExistX(ctx))
	require.False(t, client.OAuthUserInfo.Query().Where(oauthuserinfo.UserID("user-ui-expired")).ExistX(ctx))
	require.False(t, client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash("hash-rt-expired")).ExistX(ctx))
	require.False(t, client.OAuthRevokedJTI.Query().Where(oauthrevokedjti.Jti("jti-expired")).ExistX(ctx))
	require.False(t, client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID("meta-expired")).ExistX(ctx))
	_, err = client.OAuthRefreshFamily.Query().Where(oauthrefreshfamily.FamilyID("family-old-revoked")).Only(ctx)
	require.True(t, ent.IsNotFound(err))

	// Live/kept rows remain.
	require.True(t, client.OAuthAuthState.Query().Where(oauthauthstate.StateID("state-live")).ExistX(ctx))
	require.True(t, client.OAuthAuthCode.Query().Where(oauthauthcode.Code("code-live")).ExistX(ctx))
	require.True(t, client.OAuthProviderToken.Query().Where(oauthprovidertoken.UserID("user-pt-live")).ExistX(ctx))
	require.True(t, client.OAuthUserInfo.Query().Where(oauthuserinfo.UserID("user-ui-live")).ExistX(ctx))
	require.True(t, client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash("hash-rt-live")).ExistX(ctx))
	require.True(t, client.OAuthRevokedJTI.Query().Where(oauthrevokedjti.Jti("jti-live")).ExistX(ctx))
	require.True(t, client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID("meta-live")).ExistX(ctx))
	require.True(t, client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID("meta-noexpiry")).ExistX(ctx))
	require.True(t, client.OAuthRefreshFamily.Query().Where(oauthrefreshfamily.FamilyID("family-recent-revoked")).ExistX(ctx))
	require.True(t, client.OAuthRefreshFamily.Query().Where(oauthrefreshfamily.FamilyID("family-active")).ExistX(ctx))
}
