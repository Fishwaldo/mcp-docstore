package index

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open("sqlite", "file:idx-"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newIndex(t *testing.T) *search.Index {
	t.Helper()
	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestRebuildAllThenSearch(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()

	ten, err := s.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u, err := s.UpsertUser(ctx, ten.ID, "sub-1", "a@acme.com", false)
	require.NoError(t, err)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}

	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "needle", Overview: "ov", Body: "haystack"})
	require.NoError(t, err)

	require.NoError(t, svc.RebuildAll(ctx))

	res, err := idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, d.ID.String(), res[0].DocumentID)
}

func TestReindexAndRemove(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()
	ten, _ := s.EnsureTenant(ctx, "acme", "Acme")
	u, _ := s.UpsertUser(ctx, ten.ID, "sub-1", "a@acme.com", false)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}
	p, _ := s.CreateProject(ctx, id, "P", "", "private")
	d, err := s.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "needle", Body: "b"})
	require.NoError(t, err)

	require.NoError(t, svc.Reindex(ctx, d.ID))
	res, _ := idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 1)

	require.NoError(t, svc.Remove(d.ID))
	res, _ = idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 0)
}

func TestReindexProjectReflectsArchive(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()
	ten, _ := s.EnsureTenant(ctx, "acme", "Acme")
	u, _ := s.UpsertUser(ctx, ten.ID, "sub-1", "a@acme.com", false)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}
	p, _ := s.CreateProject(ctx, id, "P", "", "private")
	_, err := s.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "needle", Body: "b"})
	require.NoError(t, err)
	require.NoError(t, svc.RebuildAll(ctx))

	// Archive the project, then re-stamp its docs in the index → excluded from search.
	require.NoError(t, s.ArchiveProject(ctx, id, p.ID))
	require.NoError(t, svc.ReindexProject(ctx, p.ID))
	res, _ := idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 0)
}

func TestRebuildAllClearsStaleEntries(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()
	ten, _ := s.EnsureTenant(ctx, "acme", "Acme")
	u, _ := s.UpsertUser(ctx, ten.ID, "sub-1", "a@acme.com", false)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}
	p, _ := s.CreateProject(ctx, id, "P", "", "private")
	_, err := s.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "needle", Body: "b"})
	require.NoError(t, err)

	// Initial rebuild indexes the one real DB document.
	require.NoError(t, svc.RebuildAll(ctx))
	res, _ := idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 1)

	// Inject a stale entry not present in the DB (e.g. a doc deleted out-of-band).
	require.NoError(t, idx.Put(search.Doc{ID: "stale", TenantID: ten.ID.String(), ProjectID: "gone", OwnerID: u.ID.String(), Visibility: "private", Title: "needle stale"}))
	res, _ = idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 2) // real + stale

	// A second RebuildAll must drop the stale entry (Reset clears the index first).
	require.NoError(t, svc.RebuildAll(ctx))
	res, _ = idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()})
	require.Len(t, res, 1)
	require.NotEqual(t, "stale", res[0].DocumentID)
}

func TestServiceSearchPassthrough(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()
	ten, _ := s.EnsureTenant(ctx, "acme", "Acme")
	u, _ := s.UpsertUser(ctx, ten.ID, "sub-1", "a@acme.com", false)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}
	p, _ := s.CreateProject(ctx, id, "P", "", "private")
	d, err := s.CreateDocument(ctx, id, p.ID, store.NewDocument{Title: "needle", Body: "b"})
	require.NoError(t, err)
	require.NoError(t, svc.Reindex(ctx, d.ID))

	q := search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: u.ID.String()}
	res, err := svc.Search(q)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, d.ID.String(), res[0].DocumentID)

	q.Text = "nomatch"
	res, err = svc.Search(q)
	require.NoError(t, err)
	require.Len(t, res, 0)
}

func TestRebuildAllSharedUserReachable(t *testing.T) {
	s := newStore(t)
	idx := newIndex(t)
	svc := New(s, idx)
	ctx := context.Background()
	ten, _ := s.EnsureTenant(ctx, "acme", "Acme")
	owner, _ := s.UpsertUser(ctx, ten.ID, "sub-owner", "o@acme.com", false)
	bob, _ := s.UpsertUser(ctx, ten.ID, "sub-bob", "bob@acme.com", false)
	ownerID := store.Identity{TenantID: ten.ID, UserID: owner.ID}
	p, _ := s.CreateProject(ctx, ownerID, "P", "", "private")
	_, err := s.CreateDocument(ctx, ownerID, p.ID, store.NewDocument{Title: "needle", Body: "b"})
	require.NoError(t, err)
	_, err = s.ShareProjectUsers(ctx, ownerID, p.ID, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)

	require.NoError(t, svc.RebuildAll(ctx))

	// Bob (individually shared) finds it through the synced index; a stranger does not.
	res, _ := idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: bob.ID.String()})
	require.Len(t, res, 1)
	res, _ = idx.Search(search.Query{Text: "needle", TenantID: ten.ID.String(), UserID: "stranger"})
	require.Len(t, res, 0)
}
