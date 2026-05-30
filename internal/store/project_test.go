package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fixture creates a tenant + one user and returns an Identity for that user.
func fixture(t *testing.T, s *Store) (context.Context, Identity) {
	t.Helper()
	ctx := context.Background()
	ten, err := s.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u, err := s.UpsertUser(ctx, ten.ID, "sub-"+uuid.NewString(), "alice@acme.com")
	require.NoError(t, err)
	return ctx, Identity{TenantID: ten.ID, UserID: u.ID}
}

func TestCreateAndGetProject(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)

	p, err := s.CreateProject(ctx, id, "Notes", "my notes", "private")
	require.NoError(t, err)
	require.Equal(t, "Notes", p.Name)

	got, err := s.GetProject(ctx, id, p.ID)
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
}

func TestGetProjectCrossTenantIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "Notes", "", "private")
	require.NoError(t, err)

	// A different tenant/user must not see it; existence is not revealed.
	other := Identity{TenantID: uuid.New(), UserID: uuid.New()}
	_, err = s.GetProject(ctx, other, p.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListProjectsShowsAccessibleOnly(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)

	// Second user in same tenant.
	other, err := s.UpsertUser(ctx, owner.TenantID, "sub-other", "bob@acme.com")
	require.NoError(t, err)
	bob := Identity{TenantID: owner.TenantID, UserID: other.ID}

	_, err = s.CreateProject(ctx, owner, "Private", "", "private")
	require.NoError(t, err)
	orgP, err := s.CreateProject(ctx, owner, "Shared", "", "org")
	require.NoError(t, err)

	list, err := s.ListProjects(ctx, bob, false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, orgP.ID, list[0].ID)
}

func TestListProjectsExcludesArchivedByDefault(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)

	active, err := s.CreateProject(ctx, owner, "Active", "", "private")
	require.NoError(t, err)
	arch, err := s.CreateProject(ctx, owner, "Archived", "", "private")
	require.NoError(t, err)
	require.NoError(t, s.ArchiveProject(ctx, owner, arch.ID))

	// Default: archived omitted.
	list, err := s.ListProjects(ctx, owner, false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, active.ID, list[0].ID)

	// include_archived=true: both returned.
	all, err := s.ListProjects(ctx, owner, true)
	require.NoError(t, err)
	require.Len(t, all, 2)

	// Direct get still works on an archived project.
	got, err := s.GetProject(ctx, owner, arch.ID)
	require.NoError(t, err)
	require.True(t, got.Archived)

	// Unarchive restores it to the default listing.
	require.NoError(t, s.UnarchiveProject(ctx, owner, arch.ID))
	list2, err := s.ListProjects(ctx, owner, false)
	require.NoError(t, err)
	require.Len(t, list2, 2)
}

func TestShareProjectWithUserGrantsAccess(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	bobEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com")
	require.NoError(t, err)
	bob := Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}

	p, err := s.CreateProject(ctx, owner, "Doc", "", "private")
	require.NoError(t, err)

	// Before sharing, bob can't see it.
	_, err = s.GetProject(ctx, bob, p.ID)
	require.ErrorIs(t, err, ErrNotFound)

	res, err := s.ShareProjectUsers(ctx, owner, p.ID, []string{"bob@acme.com", "ghost@acme.com"}, "write")
	require.NoError(t, err)
	require.Equal(t, []string{"ghost@acme.com"}, res.Unresolved)

	// Now bob has write access.
	_, got, err := s.requireAccess(ctx, bob, p.ID, WriteAccess)
	require.NoError(t, err)
	require.Equal(t, WriteAccess, got)
}

func TestShareProjectRequiresOwner(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	bobEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com")
	require.NoError(t, err)
	bob := Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}

	p, err := s.CreateProject(ctx, owner, "Doc", "", "org") // bob can read+write docs...
	require.NoError(t, err)
	// ...but only the owner/admin may manage shares.
	_, err = s.ShareProjectGroups(ctx, bob, p.ID, []string{"eng"}, "read")
	require.ErrorIs(t, err, ErrPermission)
}

func TestShareProjectNoAccessIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	// A same-tenant user with NO grant on a private project must not learn it exists:
	// owner-gated operations return ErrNotFound, not ErrPermission.
	strangerEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-stranger", "s@acme.com")
	require.NoError(t, err)
	stranger := Identity{TenantID: owner.TenantID, UserID: strangerEnt.ID}

	p, err := s.CreateProject(ctx, owner, "Secret", "", "private")
	require.NoError(t, err)

	_, err = s.ShareProjectUsers(ctx, stranger, p.ID, []string{"s@acme.com"}, "read")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, s.ArchiveProject(ctx, stranger, p.ID), ErrNotFound)
}

func TestShareProjectWithGroup(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	p, err := s.CreateProject(ctx, owner, "Doc", "", "private")
	require.NoError(t, err)

	_, err = s.ShareProjectGroups(ctx, owner, p.ID, []string{"eng"}, "read")
	require.NoError(t, err)

	stranger := Identity{TenantID: owner.TenantID, UserID: uuid.New(), Groups: []string{"eng"}}
	_, got, err := s.requireAccess(ctx, stranger, p.ID, ReadAccess)
	require.NoError(t, err)
	require.Equal(t, ReadAccess, got)
}

func TestUnshareRevokesAccess(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	bobEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com")
	require.NoError(t, err)
	bob := Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}
	p, err := s.CreateProject(ctx, owner, "Doc", "", "private")
	require.NoError(t, err)

	_, err = s.ShareProjectUsers(ctx, owner, p.ID, []string{"bob@acme.com"}, "write")
	require.NoError(t, err)
	_, _, err = s.requireAccess(ctx, bob, p.ID, WriteAccess)
	require.NoError(t, err)

	// Unshare revokes: bob can no longer see the project at all.
	require.NoError(t, s.UnshareProjectUsers(ctx, owner, p.ID, []string{"bob@acme.com"}))
	_, err = s.GetProject(ctx, bob, p.ID)
	require.ErrorIs(t, err, ErrNotFound)

	// Group unshare revokes too.
	_, err = s.ShareProjectGroups(ctx, owner, p.ID, []string{"eng"}, "read")
	require.NoError(t, err)
	grp := Identity{TenantID: owner.TenantID, UserID: uuid.New(), Groups: []string{"eng"}}
	_, _, err = s.requireAccess(ctx, grp, p.ID, ReadAccess)
	require.NoError(t, err)
	require.NoError(t, s.UnshareProjectGroups(ctx, owner, p.ID, []string{"eng"}))
	_, err = s.GetProject(ctx, grp, p.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestReshareUpdatesPermission(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	bobEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com")
	require.NoError(t, err)
	bob := Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}
	p, err := s.CreateProject(ctx, owner, "Doc", "", "private")
	require.NoError(t, err)

	// Share read, then re-share write: permission is updated, not duplicated.
	_, err = s.ShareProjectUsers(ctx, owner, p.ID, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)
	_, got, err := s.requireAccess(ctx, bob, p.ID, ReadAccess)
	require.NoError(t, err)
	require.Equal(t, ReadAccess, got)

	_, err = s.ShareProjectUsers(ctx, owner, p.ID, []string{"bob@acme.com"}, "write")
	require.NoError(t, err)
	_, got, err = s.requireAccess(ctx, bob, p.ID, WriteAccess)
	require.NoError(t, err)
	require.Equal(t, WriteAccess, got)
}
