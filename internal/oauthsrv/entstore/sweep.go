// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthcode"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthstate"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthprovidertoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshfamily"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrevokedjti"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthtokenmetadata"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthuserinfo"
)

// revokedFamilyRetention is how long a revoked OAuthRefreshFamily row is kept after
// revocation before DeleteExpired purges it. The row lingers this long so reuse-detection
// forensics (e.g. "was this family revoked before or after the replay we just saw?") can look
// back at a family that no longer has any live member tokens.
const revokedFamilyRetention = 30 * 24 * time.Hour

// DeleteExpired removes every row whose expires_at is non-zero and before now, across every
// OAuth AS table that carries an expiry: authorization states, authorization codes, cached
// provider tokens, cached userinfo, refresh tokens, revoked JTIs, and token metadata. It also
// removes OAuthRefreshFamily rows revoked more than revokedFamilyRetention ago, per the
// retention policy on RevokeRefreshTokenFamily's forensics rationale. It returns the total
// number of rows removed across all tables, for callers (a periodic sweeper, mirroring
// internal/store/session.go's DeleteExpiredSessions) to log or expose as a metric.
func (s *Store) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	var total int

	n, err := s.client.OAuthAuthState.Delete().
		Where(oauthauthstate.ExpiresAtNEQ(time.Time{}), oauthauthstate.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthAuthCode.Delete().
		Where(oauthauthcode.ExpiresAtNEQ(time.Time{}), oauthauthcode.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthProviderToken.Delete().
		Where(oauthprovidertoken.ExpiresAtNEQ(time.Time{}), oauthprovidertoken.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthUserInfo.Delete().
		Where(oauthuserinfo.ExpiresAtNEQ(time.Time{}), oauthuserinfo.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthRefreshToken.Delete().
		Where(oauthrefreshtoken.ExpiresAtNEQ(time.Time{}), oauthrefreshtoken.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthRevokedJTI.Delete().
		Where(oauthrevokedjti.ExpiresAtNEQ(time.Time{}), oauthrevokedjti.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	// OAuthTokenMetadata.expires_at is optional (a zero/unset expiry means "unknown", not
	// "expired"), so IsNil rows must be excluded explicitly rather than relying on the
	// ExpiresAtNEQ(zero) check the required-field tables above use.
	n, err = s.client.OAuthTokenMetadata.Delete().
		Where(oauthtokenmetadata.ExpiresAtNotNil(), oauthtokenmetadata.ExpiresAtLT(now)).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	n, err = s.client.OAuthRefreshFamily.Delete().
		Where(oauthrefreshfamily.RevokedAtNotNil(), oauthrefreshfamily.RevokedAtLT(now.Add(-revokedFamilyRetention))).
		Exec(ctx)
	if err != nil {
		return total, err
	}
	total += n

	return total, nil
}
