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
	res, err := svc.Search(id, search.Query{Text: "hello"})
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
	res, err := svc.Search(id, search.Query{Text: "new"})
	require.NoError(t, err)
	require.Len(t, res, 1)
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
	res, err := svc.Search(id, search.Query{Text: "hello"})
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
	res, err := svc.Search(id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Empty(t, res)
	// unarchive restores visibility
	require.NoError(t, svc.UnarchiveProject(ctx, id, pid))
	res, err = svc.Search(id, search.Query{Text: "hello"})
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestDeleteProjectEvictsDocsFromIndex(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	ctx := context.Background()
	_, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "hello"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteProject(ctx, id, pid))
	res, err := svc.Search(id, search.Query{Text: "hello"})
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
	res, err := svc.Search(bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Empty(t, res)
	// share read with bob -> reindex -> now findable
	sr, err := svc.ShareUsers(ctx, owner, pid, []string{"bob@acme.com"}, "read")
	require.NoError(t, err)
	require.Empty(t, sr.Unresolved)
	res, err = svc.Search(bob, search.Query{Text: "secret"})
	require.NoError(t, err)
	require.Len(t, res, 1)
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
