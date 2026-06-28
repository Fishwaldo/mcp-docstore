// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateGetListDocument(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "Proj", "", "private")
	require.NoError(t, err)

	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{
		Title: "Design", Overview: "the overview", Body: "# Title\n\nbody",
		Tags: []string{"spec"}, Comment: "initial",
	})
	require.NoError(t, err)
	require.Equal(t, 1, d.Version)
	require.Equal(t, id.TenantID, d.TenantID)

	got, err := s.GetDocument(ctx, id, d.ID)
	require.NoError(t, err)
	require.Equal(t, "Design", got.Title)

	list, err := s.ListDocuments(ctx, id, p.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestCreateDocumentRequiresWriteAccess(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	readerEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-r", "r@acme.com", false)
	require.NoError(t, err)
	reader := Identity{TenantID: owner.TenantID, UserID: readerEnt.ID}

	p, err := s.CreateProject(ctx, owner, "Proj", "", "private")
	require.NoError(t, err)
	_, err = s.ShareProjectUsers(ctx, owner, p.ID, []string{"r@acme.com"}, "read")
	require.NoError(t, err)

	_, err = s.CreateDocument(ctx, reader, p.ID, NewDocument{Title: "x", Overview: "o", Body: "b"})
	require.ErrorIs(t, err, ErrPermission)
}

func TestGetDocumentCrossTenantIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "Proj", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b"})
	require.NoError(t, err)

	// A different tenant must not see the document; existence is not revealed.
	other := Identity{TenantID: uuid.New(), UserID: uuid.New()}
	_, err = s.GetDocument(ctx, other, d.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListDocumentsScopedToProject(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p1, err := s.CreateProject(ctx, id, "P1", "", "private")
	require.NoError(t, err)
	p2, err := s.CreateProject(ctx, id, "P2", "", "private")
	require.NoError(t, err)

	d1, err := s.CreateDocument(ctx, id, p1.ID, NewDocument{Title: "in-p1", Overview: "o", Body: "b"})
	require.NoError(t, err)
	_, err = s.CreateDocument(ctx, id, p2.ID, NewDocument{Title: "in-p2", Overview: "o", Body: "b"})
	require.NoError(t, err)

	list, err := s.ListDocuments(ctx, id, p1.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, d1.ID, list[0].ID)
}

func TestEditDocumentSnapshotsAndBumpsVersion(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o1", Body: "b1"})
	require.NoError(t, err)

	body2 := "b2"
	d2, err := s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: 1, Body: &body2, Comment: "second"})
	require.NoError(t, err)
	require.Equal(t, 2, d2.Version)
	require.Equal(t, "b2", d2.Body)
	require.Equal(t, "o1", d2.Overview) // unchanged field preserved

	snaps, err := s.ListSnapshots(ctx, id, d.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	require.Equal(t, 1, snaps[0].Version) // prior state captured
	require.Equal(t, "b1", snaps[0].Body)
}

func TestEditDocumentStaleVersionConflicts(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b"})
	require.NoError(t, err)

	body := "x"
	_, err = s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: 99, Body: &body})
	require.ErrorIs(t, err, ErrConflict)
}

func TestSnapshotPruningRespectsRetention(t *testing.T) {
	s, err := Open("sqlite", "file:prune?mode=memory&cache=shared&_pragma=foreign_keys(1)", WithSnapshotRetention(2))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "v1"})
	require.NoError(t, err)

	for i := 2; i <= 5; i++ {
		body := "v" + string(rune('0'+i))
		_, err = s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: i - 1, Body: &body})
		require.NoError(t, err)
	}
	snaps, err := s.ListSnapshots(ctx, id, d.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 2) // only the 2 most recent retained
	require.Equal(t, 4, snaps[0].Version)
	require.Equal(t, 3, snaps[1].Version)
}

func TestGetAndRestoreSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "v1"})
	require.NoError(t, err)
	body2 := "v2"
	_, err = s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: 1, Body: &body2})
	require.NoError(t, err)

	snap, err := s.GetSnapshot(ctx, id, d.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "v1", snap.Body)

	// Restore v1 as a new edit; current is v2 so base_version must be 2.
	restored, err := s.RestoreSnapshot(ctx, id, d.ID, 1, 2, RestoreScopeFull, "revert to v1")
	require.NoError(t, err)
	require.Equal(t, 3, restored.Version)
	require.Equal(t, "v1", restored.Body)
}

func TestDeleteDocument(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b"})
	require.NoError(t, err)

	// Edit so the document has a snapshot — delete must cascade to it (FK constraint).
	body2 := "b2"
	_, err = s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: 1, Body: &body2})
	require.NoError(t, err)
	snaps, err := s.ListSnapshots(ctx, id, d.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 1)

	require.NoError(t, s.DeleteDocument(ctx, id, d.ID))
	_, err = s.GetDocument(ctx, id, d.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestEditLoadedMatchesEditDocument(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o1", Body: "b1"})
	require.NoError(t, err)

	loaded, err := s.loadDocument(ctx, id, d.ID)
	require.NoError(t, err)
	body2 := "b2"
	d2, err := s.EditLoaded(ctx, id, loaded, EditDocument{BaseVersion: 1, Body: &body2, Comment: "second"})
	require.NoError(t, err)
	require.Equal(t, 2, d2.Version)
	require.Equal(t, "b2", d2.Body)
	require.Equal(t, "o1", d2.Overview) // unchanged field preserved

	snaps, err := s.ListSnapshots(ctx, id, d.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	require.Equal(t, 1, snaps[0].Version)
	require.Equal(t, "b1", snaps[0].Body)
}

func TestEditLoadedStaleVersionConflicts(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b"})
	require.NoError(t, err)
	loaded, err := s.loadDocument(ctx, id, d.ID)
	require.NoError(t, err)

	body := "x"
	_, err = s.EditLoaded(ctx, id, loaded, EditDocument{BaseVersion: 99, Body: &body})
	require.ErrorIs(t, err, ErrConflict)
}

func TestEditLoadedRequiresWriteAccess(t *testing.T) {
	s := newTestStore(t)
	ctx, owner := fixture(t, s)
	readerEnt, err := s.UpsertUser(ctx, owner.TenantID, "sub-r", "r@acme.com", false)
	require.NoError(t, err)
	reader := Identity{TenantID: owner.TenantID, UserID: readerEnt.ID}
	p, err := s.CreateProject(ctx, owner, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, owner, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b"})
	require.NoError(t, err)
	_, err = s.ShareProjectUsers(ctx, owner, p.ID, []string{"r@acme.com"}, "read")
	require.NoError(t, err)

	// Reader loads the doc (read access) but EditLoaded must reject the write.
	loaded, err := s.loadDocument(ctx, reader, d.ID)
	require.NoError(t, err)
	body := "x"
	_, err = s.EditLoaded(ctx, reader, loaded, EditDocument{BaseVersion: 1, Body: &body})
	require.ErrorIs(t, err, ErrPermission)
}

func TestRestoreSnapshotBodyOnlyPreservesTags(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{
		Title: "T", Overview: "ov1", Body: "body1", Tags: []string{"status:not-started"},
	})
	require.NoError(t, err)
	body2, ov2, tags2 := "body2", "ov2", []string{"status:in-progress"}
	d2, err := s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: d.Version, Body: &body2, Overview: &ov2, Tags: &tags2, Comment: "v2"})
	require.NoError(t, err)
	got, err := s.RestoreSnapshot(ctx, id, d.ID, d.Version, d2.Version, RestoreScopeBody, "restore")
	require.NoError(t, err)
	if got.Body != "body1" {
		t.Errorf("body = %q want body1", got.Body)
	}
	if got.Overview != "ov2" {
		t.Errorf("overview = %q want ov2 (preserved)", got.Overview)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "status:in-progress" {
		t.Errorf("tags = %v want [status:in-progress] (preserved)", got.Tags)
	}
}

func TestRestoreSnapshotFullRestoresAll(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "ov1", Body: "body1", Tags: []string{"status:not-started"}})
	require.NoError(t, err)
	body2, ov2, tags2 := "body2", "ov2", []string{"status:in-progress"}
	d2, err := s.EditDocument(ctx, id, d.ID, EditDocument{BaseVersion: d.Version, Body: &body2, Overview: &ov2, Tags: &tags2, Comment: "v2"})
	require.NoError(t, err)
	got, err := s.RestoreSnapshot(ctx, id, d.ID, d.Version, d2.Version, RestoreScopeFull, "restore")
	require.NoError(t, err)
	if got.Body != "body1" || got.Overview != "ov1" || got.Tags[0] != "status:not-started" {
		t.Errorf("full restore did not revert all fields: body=%q ov=%q tags=%v", got.Body, got.Overview, got.Tags)
	}
}
