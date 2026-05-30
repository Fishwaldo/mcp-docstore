package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/document"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/documentsnapshot"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/project"
)

// NewDocument holds the fields for creating a new document.
type NewDocument struct {
	Title    string
	Overview string
	Body     string
	Tags     []string
	Comment  string
}

// CreateDocument creates a new document under the given project.
// Validates that title is non-empty and that the caller has WriteAccess on the project.
// tenant_id is denormalized from id.TenantID; version defaults to 1 (ent schema default).
func (s *Store) CreateDocument(ctx context.Context, id Identity, projectID uuid.UUID, in NewDocument) (*ent.Document, error) {
	if in.Title == "" {
		return nil, fmt.Errorf("%w: title required", ErrInvalid)
	}
	if _, _, err := s.requireAccess(ctx, id, projectID, WriteAccess); err != nil {
		return nil, err
	}
	return s.client.Document.Create().
		SetProjectID(projectID).
		SetTenantID(id.TenantID).
		SetTitle(in.Title).
		SetOverview(in.Overview).
		SetBody(in.Body).
		SetTags(in.Tags).
		SetChangeComment(in.Comment).
		SetCreatedByID(id.UserID).
		SetUpdatedByID(id.UserID).
		Save(ctx)
}

// loadDocument fetches a document filtered by ID and tenant_id, with its project
// (and shares) eager-loaded for access checks.
// Returns ErrNotFound if the document is missing or belongs to another tenant.
func (s *Store) loadDocument(ctx context.Context, id Identity, documentID uuid.UUID) (*ent.Document, error) {
	d, err := s.client.Document.Query().
		Where(document.IDEQ(documentID), document.TenantIDEQ(id.TenantID)).
		WithProject(func(q *ent.ProjectQuery) {
			q.WithOwner().
				WithShares(func(sq *ent.ProjectShareQuery) { sq.WithUser() }).
				WithGroupShares().
				WithTenant()
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// documentAccess computes effective access from the document's loaded project edges.
// NoAccess → ErrNotFound (hides existence); insufficient → ErrPermission.
func (s *Store) documentAccess(d *ent.Document, id Identity, need Access) error {
	got := effectiveAccess(factsOf(d.Edges.Project), id)
	if got == NoAccess {
		return ErrNotFound
	}
	if got < need {
		return ErrPermission
	}
	return nil
}

// GetDocument returns a single document visible to the caller.
func (s *Store) GetDocument(ctx context.Context, id Identity, documentID uuid.UUID) (*ent.Document, error) {
	d, err := s.loadDocument(ctx, id, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.documentAccess(d, id, ReadAccess); err != nil {
		return nil, err
	}
	return d, nil
}

// ListDocuments returns all documents in the project visible to the caller.
// Requires ReadAccess on the project, then returns documents scoped to the
// caller's tenant and belonging to the given project.
func (s *Store) ListDocuments(ctx context.Context, id Identity, projectID uuid.UUID) ([]*ent.Document, error) {
	if _, _, err := s.requireAccess(ctx, id, projectID, ReadAccess); err != nil {
		return nil, err
	}
	return s.client.Document.Query().
		Where(
			document.TenantIDEQ(id.TenantID),
			document.HasProjectWith(project.IDEQ(projectID)),
		).
		All(ctx)
}

// EditDocument holds the fields for an optimistic-concurrency edit.
type EditDocument struct {
	BaseVersion int
	// nil pointer = leave field unchanged.
	Overview *string
	Body     *string
	Tags     *[]string
	Comment  string
}

// EditDocument applies an optimistic-concurrency edit: it verifies the caller's
// base_version matches the current version (ErrConflict otherwise), snapshots the
// prior state, applies the supplied changes (pointer-is-set semantics), bumps the
// version, and prunes snapshots beyond retention. The whole operation is transactional.
func (s *Store) EditDocument(ctx context.Context, id Identity, documentID uuid.UUID, in EditDocument) (*ent.Document, error) {
	d, err := s.loadDocument(ctx, id, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.documentAccess(d, id, WriteAccess); err != nil {
		return nil, err
	}
	if d.Version != in.BaseVersion {
		return nil, fmt.Errorf("%w: current version is %d", ErrConflict, d.Version)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Snapshot the prior state.
	if _, err := tx.DocumentSnapshot.Create().
		SetDocumentID(d.ID).
		SetVersion(d.Version).
		SetOverview(d.Overview).
		SetBody(d.Body).
		SetTags(d.Tags).
		SetComment(d.ChangeComment).
		SetCreatedByID(id.UserID).
		Save(ctx); err != nil {
		return nil, err
	}

	// 2. Apply changes + bump version, guarded by the base version so the write is
	//    atomic under concurrency: only the row still at base_version is updated.
	//    Zero rows affected means another writer moved the version first → conflict.
	upd := tx.Document.Update().
		Where(document.IDEQ(d.ID), document.VersionEQ(in.BaseVersion)).
		SetVersion(in.BaseVersion + 1).
		SetChangeComment(in.Comment).
		SetUpdatedByID(id.UserID)
	if in.Overview != nil {
		upd.SetOverview(*in.Overview)
	}
	if in.Body != nil {
		upd.SetBody(*in.Body)
	}
	if in.Tags != nil {
		upd.SetTags(*in.Tags)
	}
	affected, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, fmt.Errorf("%w: document was modified concurrently", ErrConflict)
	}
	updated, err := tx.Document.Get(ctx, d.ID)
	if err != nil {
		return nil, err
	}

	// 3. Prune snapshots beyond retention (keep newest N by version).
	if err := s.pruneSnapshots(ctx, tx, d.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

// pruneSnapshots deletes all but the newest `retention` snapshots (by version) for
// the given document, within the supplied transaction.
func (s *Store) pruneSnapshots(ctx context.Context, tx *ent.Tx, documentID uuid.UUID) error {
	ids, err := tx.DocumentSnapshot.Query().
		Where(documentsnapshot.HasDocumentWith(document.IDEQ(documentID))).
		Order(ent.Desc(documentsnapshot.FieldVersion)).
		IDs(ctx)
	if err != nil {
		return err
	}
	if len(ids) <= s.retention {
		return nil
	}
	old := ids[s.retention:] // everything past the newest `retention`
	_, err = tx.DocumentSnapshot.Delete().Where(documentsnapshot.IDIn(old...)).Exec(ctx)
	return err
}

// ListSnapshots returns the document's snapshots newest-first. Requires ReadAccess.
func (s *Store) ListSnapshots(ctx context.Context, id Identity, documentID uuid.UUID) ([]*ent.DocumentSnapshot, error) {
	d, err := s.loadDocument(ctx, id, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.documentAccess(d, id, ReadAccess); err != nil {
		return nil, err
	}
	return s.client.DocumentSnapshot.Query().
		Where(documentsnapshot.HasDocumentWith(document.IDEQ(documentID))).
		Order(ent.Desc(documentsnapshot.FieldVersion)).
		All(ctx)
}

// GetSnapshot returns a specific historical snapshot by version. Requires ReadAccess.
// Returns ErrNotFound if the snapshot does not exist.
func (s *Store) GetSnapshot(ctx context.Context, id Identity, documentID uuid.UUID, version int) (*ent.DocumentSnapshot, error) {
	d, err := s.loadDocument(ctx, id, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.documentAccess(d, id, ReadAccess); err != nil {
		return nil, err
	}
	snap, err := s.client.DocumentSnapshot.Query().
		Where(
			documentsnapshot.HasDocumentWith(document.IDEQ(documentID)),
			documentsnapshot.VersionEQ(version),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return snap, err
}

// RestoreSnapshot makes version restoreVersion current by calling EditDocument with its
// content at baseVersion. This produces a new versioned edit (snapshotted + concurrency-checked).
// If comment is empty, a default "restored from version N" comment is used.
func (s *Store) RestoreSnapshot(ctx context.Context, id Identity, documentID uuid.UUID, restoreVersion, baseVersion int, comment string) (*ent.Document, error) {
	snap, err := s.GetSnapshot(ctx, id, documentID, restoreVersion)
	if err != nil {
		return nil, err
	}
	tags := snap.Tags
	if comment == "" {
		comment = fmt.Sprintf("restored from version %d", restoreVersion)
	}
	return s.EditDocument(ctx, id, documentID, EditDocument{
		BaseVersion: baseVersion,
		Overview:    &snap.Overview,
		Body:        &snap.Body,
		Tags:        &tags,
		Comment:     comment,
	})
}

// DeleteDocument permanently removes a document. Requires WriteAccess.
func (s *Store) DeleteDocument(ctx context.Context, id Identity, documentID uuid.UUID) error {
	d, err := s.loadDocument(ctx, id, documentID)
	if err != nil {
		return err
	}
	if err := s.documentAccess(d, id, WriteAccess); err != nil {
		return err
	}
	return s.client.Document.DeleteOneID(documentID).Exec(ctx)
}
