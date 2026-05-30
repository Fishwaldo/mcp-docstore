package store

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEffectiveAccess(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	sharedUser := uuid.New()

	proj := projectFacts{
		Visibility: "private",
		OwnerID:    owner,
		UserShares: map[uuid.UUID]string{sharedUser: "read"},
		GroupShares: map[string]string{"eng": "write"},
	}

	tests := []struct {
		name   string
		ident  Identity
		facts  projectFacts
		want   Access
	}{
		{"owner has write", Identity{UserID: owner}, proj, WriteAccess},
		{"stranger has none", Identity{UserID: other}, proj, NoAccess},
		{"individual read share", Identity{UserID: sharedUser}, proj, ReadAccess},
		{"group write share", Identity{UserID: other, Groups: []string{"eng"}}, proj, WriteAccess},
		{"org grants write to all", Identity{UserID: other}, projectFacts{Visibility: "org", OwnerID: owner}, WriteAccess},
		{"highest wins: read share + group write", Identity{UserID: sharedUser, Groups: []string{"eng"}}, proj, WriteAccess},
		{"admin override", Identity{UserID: other, IsAdmin: true}, proj, WriteAccess},
		// Security edge cases: shares must grant exactly their level, no more.
		{"individual write share grants write", Identity{UserID: sharedUser},
			projectFacts{Visibility: "private", OwnerID: owner, UserShares: map[uuid.UUID]string{sharedUser: "write"}}, WriteAccess},
		{"group read share grants only read", Identity{UserID: other, Groups: []string{"eng"}},
			projectFacts{Visibility: "private", OwnerID: owner, GroupShares: map[string]string{"eng": "read"}}, ReadAccess},
		{"non-matching group gets nothing", Identity{UserID: other, Groups: []string{"hr"}}, proj, NoAccess},
		{"nil share maps are safe", Identity{UserID: other}, projectFacts{Visibility: "private", OwnerID: owner}, NoAccess},
		{"unknown permission string fails closed", Identity{UserID: sharedUser},
			projectFacts{Visibility: "private", OwnerID: owner, UserShares: map[uuid.UUID]string{sharedUser: "superadmin"}}, NoAccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, effectiveAccess(tc.facts, tc.ident))
		})
	}
}
