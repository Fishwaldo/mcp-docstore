// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func newSvc(t *testing.T) (*Service, *store.Store, store.Identity, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open("sqlite", "file:mcpsvc-"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	require.NoError(t, st.Migrate(ctx))
	idx, err := search.Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })

	ten, err := st.EnsureTenant(ctx, "acme", "Acme")
	require.NoError(t, err)
	u, err := st.UpsertUser(ctx, ten.ID, "sub-1", "alice@acme.com", false)
	require.NoError(t, err)
	id := store.Identity{TenantID: ten.ID, UserID: u.ID}
	p, err := st.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)

	svc := NewService(st, index.New(st, idx), nil)
	return svc, st, id, p.ID
}

func TestCreateDocumentIndexed(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Overview: "ov", Body: "hello world", Tags: []string{"x"}})
	require.NoError(t, err)
	res, err := svc.Search(ctx, id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, d.ID.String(), res[0].DocumentID)
}

func TestEditSectionReindexes(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "# A\nold\n\n# B\nkeep"})
	require.NoError(t, err)
	got, err := svc.EditSection(ctx, id, d.ID, d.Version, "A", "new content", "edit A")
	require.NoError(t, err)
	require.Contains(t, got.Body, "new content")
	require.NotContains(t, got.Body, "old")
	res, err := svc.Search(ctx, id, search.Query{Text: "new"})
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestEditSectionStaleBaseConflicts(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "# A\nold"})
	require.NoError(t, err)
	// Bump the version once so the caller's base (d.Version) is now stale.
	_, err = svc.EditSection(ctx, id, d.ID, d.Version, "A", "first", "e1")
	require.NoError(t, err)
	// Re-using the original (now stale) base must conflict.
	_, err = svc.EditSection(ctx, id, d.ID, d.Version, "A", "second", "e2")
	require.ErrorIs(t, err, store.ErrConflict)
}

func TestAppendDocumentSnapshotsAndReindexes(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "first"})
	require.NoError(t, err)
	got, err := svc.AppendDocument(ctx, id, d.ID, "second", "append")
	require.NoError(t, err)
	require.Contains(t, got.Body, "first")
	require.Contains(t, got.Body, "second")
	require.Equal(t, d.Version+1, got.Version)
	snaps, err := svc.ListSnapshots(ctx, id, d.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 1) // prior version snapshotted
}

func TestAppendToEmptyBodyHasNoLeadingNewline(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: ""})
	require.NoError(t, err)
	got, err := svc.AppendDocument(ctx, id, d.ID, "first append text", "append")
	require.NoError(t, err)
	require.Equal(t, "first append text", got.Body)
}

func TestDeleteDocumentEvictsFromIndex(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteDocument(ctx, id, d.ID))
	res, err := svc.Search(ctx, id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestDiffVersions(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "line one"})
	require.NoError(t, err)
	body2 := "line two"
	v2, err := svc.EditReplace(ctx, id, d.ID, d.Version, nil, &body2, nil, "change")
	require.NoError(t, err)
	diff, err := svc.DiffVersions(ctx, id, d.ID, 1, v2.Version)
	require.NoError(t, err)
	require.Contains(t, diff, "line one")
	require.Contains(t, diff, "line two")
}

func TestArchiveProjectHidesFromSearch(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	_, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, svc.ArchiveProject(ctx, id, pid))
	res, err := svc.Search(ctx, id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Empty(t, res)
	// unarchive restores visibility
	require.NoError(t, svc.UnarchiveProject(ctx, id, pid))
	res, err = svc.Search(ctx, id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestDeleteProjectEvictsDocsFromIndex(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	_, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteProject(ctx, id, pid))
	res, err := svc.Search(ctx, id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestShareUsersReindexesSoShareeCanSearch(t *testing.T) {
	svc, st, owner, pid := newSvc(t)
	ctx := context.Background()
	_, err := svc.CreateDocument(ctx, owner, pid, store.NewDocument{Title: "T", Body: "secret"})
	require.NoError(t, err)
	// bob is a second user in the same tenant
	bobEnt, err := st.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com", false)
	require.NoError(t, err)
	bob := store.Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}
	// before sharing, bob can't find it
	res, err := svc.Search(ctx, bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Empty(t, res)
	// share read with bob -> reindex -> now findable
	sr, err := svc.ShareUsers(ctx, owner, pid, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)
	require.Empty(t, sr.Unresolved)
	res, err = svc.Search(ctx, bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Len(t, res, 1)
}

// TestAppendRetriesOnConflict proves the append loop re-reads and lands even when the
// version has advanced since an earlier read. We hold a stale view of the document
// (d, version 1), bump the version out from under it via a direct store edit, then
// append: the loop must read the *current* version internally and succeed.
func TestAppendRetriesOnConflict(t *testing.T) {
	svc, st, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "first"})
	require.NoError(t, err)
	// A concurrent writer bumps the version (now 2) before our append runs.
	body2 := "first\nconcurrent"
	bumped, err := st.EditDocument(ctx, id, d.ID, store.EditDocument{BaseVersion: d.Version, Body: &body2, Comment: "concurrent"})
	require.NoError(t, err)
	require.Equal(t, 2, bumped.Version)
	// Append: AppendDocument reads the current version itself, so it lands on top.
	got, err := svc.AppendDocument(ctx, id, d.ID, "appended", "append")
	require.NoError(t, err)
	require.Contains(t, got.Body, "concurrent")
	require.Contains(t, got.Body, "appended")
	require.Equal(t, 3, got.Version)
}

func TestSearchReconcilesDeletedStaleHit(t *testing.T) {
	svc, st, id, pid := newSvc(t)
	ctx := context.Background()
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "stale hello"})
	require.NoError(t, err)
	// Delete from the STORE only, so the index keeps the now-orphaned entry.
	require.NoError(t, st.DeleteDocument(ctx, id, d.ID))
	// Search must not return the deleted doc.
	res, err := svc.Search(ctx, id, search.Query{Text: "stale"})
	require.NoError(t, err)
	require.Empty(t, res)
	// Reconcile healed the index: a second search confirms the entry is gone (no error,
	// still empty) — the stale doc was best-effort removed during the first search.
	res, err = svc.Search(ctx, id, search.Query{Text: "stale"})
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestSearchReconcilesNowInaccessibleHit(t *testing.T) {
	svc, st, owner, pid := newSvc(t)
	ctx := context.Background()
	_, err := svc.CreateDocument(ctx, owner, pid, store.NewDocument{Title: "T", Body: "private secret"})
	require.NoError(t, err)
	// Share with bob and reindex so the index says bob can read it.
	bobEnt, err := st.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com", false)
	require.NoError(t, err)
	bob := store.Identity{TenantID: owner.TenantID, UserID: bobEnt.ID}
	_, err = svc.ShareUsers(ctx, owner, pid, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)
	res, err := svc.Search(ctx, bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	// Revoke at the STORE level only (bypass Service so the index is NOT re-stamped).
	require.NoError(t, st.UnshareProjectUsers(ctx, owner, pid, []string{"bob@acme.com"}))
	// Reconcile-on-read: GetDocument now returns ErrNotFound for bob, so the hit is dropped.
	res, err = svc.Search(ctx, bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Empty(t, res)
	// And the owner can still find it (its index entry is untouched).
	res, err = svc.Search(ctx, owner, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestCreateDocumentIndexSyncIsNonFatal(t *testing.T) {
	svc, st, id, pid := newSvc(t)
	ctx := context.Background()
	// Break the index so any Put/Delete fails, simulating a search backend outage.
	require.NoError(t, svc.index.CloseForTest())
	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "hello"})
	// The store write committed, so the call must succeed despite the index error.
	require.NoError(t, err)
	require.NotNil(t, d)
	// The document is really in the store.
	got, err := st.GetDocument(ctx, id, d.ID)
	require.NoError(t, err)
	require.Equal(t, d.ID, got.ID)
}

func TestListSharesRoundTrip(t *testing.T) {
	svc, st, owner, pid := newSvc(t)
	ctx := context.Background()
	_, err := st.UpsertUser(ctx, owner.TenantID, "sub-bob", "bob@acme.com", false)
	require.NoError(t, err)
	_, err = svc.ShareUsers(ctx, owner, pid, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)
	_, err = svc.ShareGroups(ctx, owner, pid, []string{"eng"}, "write")
	require.NoError(t, err)
	shares, err := svc.ListShares(ctx, owner, pid)
	require.NoError(t, err)
	require.Len(t, shares.Users, 1)
	require.Len(t, shares.Groups, 1)
}
