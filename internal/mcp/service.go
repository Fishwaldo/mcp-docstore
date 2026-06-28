// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package mcp wires the store, search index, and goldmark editing into the MCP tool
// surface. The Service bundles each store mutation with its search-index sync and any
// markdown orchestration; all methods take a resolved store.Identity.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/docs"
	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/index"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type Service struct {
	store *store.Store
	index *index.Service
	log   *slog.Logger
}

func NewService(st *store.Store, idx *index.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, index: idx, log: log}
}

// syncIndex runs an index-sync op after a store mutation has already committed. The DB
// write is the source of truth; a search-index failure here is logged and swallowed
// (returning it would misrepresent state — the row exists but the caller would be told
// the operation failed). A reconcile-on-read pass in Search heals any drift this leaves.
func (s *Service) syncIndex(op string, id fmt.Stringer, fn func() error) {
	if err := fn(); err != nil {
		s.log.Error("index sync failed", "op", op, "id", id.String(), "err", err)
	}
}

// ---- documents: reads ----

func (s *Service) GetDocument(ctx context.Context, id store.Identity, docID uuid.UUID) (*ent.Document, error) {
	return s.store.GetDocument(ctx, id, docID)
}

func (s *Service) ListDocuments(ctx context.Context, id store.Identity, projectID uuid.UUID) ([]*ent.Document, error) {
	return s.store.ListDocuments(ctx, id, projectID)
}

func (s *Service) GetSection(ctx context.Context, id store.Identity, docID uuid.UUID, heading string) (string, error) {
	d, err := s.store.GetDocument(ctx, id, docID)
	if err != nil {
		return "", err
	}
	return docs.GetSection(d.Body, heading)
}

func (s *Service) ListSnapshots(ctx context.Context, id store.Identity, docID uuid.UUID) ([]*ent.DocumentSnapshot, error) {
	return s.store.ListSnapshots(ctx, id, docID)
}

// EnsureDocumentWritable verifies the caller has write access to the document, returning
// store.ErrPermission (lacks write) or store.ErrNotFound (no access / does not exist).
// Used to gate a destructive tool before it prompts for confirmation, so a caller who
// can't perform the action is rejected without an elicitation round-trip.
func (s *Service) EnsureDocumentWritable(ctx context.Context, id store.Identity, docID uuid.UUID) error {
	return s.store.EnsureDocumentWritable(ctx, id, docID)
}

// EnsureProjectOwner verifies the caller owns the project (or is a tenant admin),
// returning store.ErrPermission or store.ErrNotFound. Gates delete_project before its
// confirmation prompt.
func (s *Service) EnsureProjectOwner(ctx context.Context, id store.Identity, projectID uuid.UUID) error {
	return s.store.EnsureProjectOwner(ctx, id, projectID)
}

func (s *Service) GetSnapshot(ctx context.Context, id store.Identity, docID uuid.UUID, version int) (*ent.DocumentSnapshot, error) {
	return s.store.GetSnapshot(ctx, id, docID, version)
}

// DiffVersions returns a unified diff of the document body between two versions. Each
// version is resolved from the current document or a retained snapshot (read-access gated
// via GetDocument); an unavailable version surfaces the store's error.
func (s *Service) DiffVersions(ctx context.Context, id store.Identity, docID uuid.UUID, fromV, toV int) (string, error) {
	d, err := s.store.GetDocument(ctx, id, docID)
	if err != nil {
		return "", err
	}
	fromBody, err := s.bodyAt(ctx, id, d, fromV)
	if err != nil {
		return "", err
	}
	toBody, err := s.bodyAt(ctx, id, d, toV)
	if err != nil {
		return "", err
	}
	return docs.UnifiedDiff(fmt.Sprintf("v%d", fromV), fmt.Sprintf("v%d", toV), fromBody, toBody), nil
}

func (s *Service) bodyAt(ctx context.Context, id store.Identity, d *ent.Document, version int) (string, error) {
	if version == d.Version {
		return d.Body, nil
	}
	snap, err := s.store.GetSnapshot(ctx, id, d.ID, version)
	if err != nil {
		return "", err
	}
	return snap.Body, nil
}

// ---- documents: mutations (each Reindexes; delete Removes) ----

func (s *Service) CreateDocument(ctx context.Context, id store.Identity, projectID uuid.UUID, in store.NewDocument) (*ent.Document, error) {
	d, err := s.store.CreateDocument(ctx, id, projectID, in)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex", d.ID, func() error { return s.index.Reindex(ctx, d.ID) })
	return d, nil
}

// EditReplace performs a full-field replace edit (optimistic via base). nil pointers leave
// a field unchanged.
func (s *Service) EditReplace(ctx context.Context, id store.Identity, docID uuid.UUID, base int, overview, body *string, tags *[]string, comment string) (*ent.Document, error) {
	d, err := s.store.EditDocument(ctx, id, docID, store.EditDocument{BaseVersion: base, Overview: overview, Body: body, Tags: tags, Comment: comment})
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex", d.ID, func() error { return s.index.Reindex(ctx, d.ID) })
	return d, nil
}

// editLoadedAndIndex runs an optimistic edit on an already-loaded document, then
// re-indexes it. It is the load-once counterpart of EditReplace for callers
// (section edit, delete-section, append) that have already fetched the document
// to compute the new body — avoiding a second store load per edit. The reindex is
// routed through syncIndex (non-fatal, logged + swallowed) to match the convention
// EditReplace and every other mutation method use, so the section/append paths
// don't reintroduce a fatal post-commit index error.
func (s *Service) editLoadedAndIndex(ctx context.Context, id store.Identity, d *ent.Document, in store.EditDocument) (*ent.Document, error) {
	updated, err := s.store.EditLoaded(ctx, id, d, in)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex", updated.ID, func() error { return s.index.Reindex(ctx, updated.ID) })
	return updated, nil
}

// EditSection replaces the body section under heading (optimistic via base).
func (s *Service) EditSection(ctx context.Context, id store.Identity, docID uuid.UUID, base int, heading, content, comment string) (*ent.Document, error) {
	d, err := s.store.GetDocument(ctx, id, docID)
	if err != nil {
		return nil, err
	}
	newBody, err := docs.ReplaceSection(d.Body, heading, content)
	if err != nil {
		return nil, err
	}
	return s.editLoadedAndIndex(ctx, id, d, store.EditDocument{BaseVersion: base, Body: &newBody, Comment: comment})
}

// AppendDocument appends text to the end of the body. Non-clobbering: it does NOT take a
// caller base_version; it reads the current version and uses it as the base, so concurrent
// appends never conflict. If another writer bumps the version between this read and the
// write, the resulting ErrConflict is retried by re-reading the current state, up to
// maxAppendAttempts times. Each successful append produces a new version + snapshot.
func (s *Service) AppendDocument(ctx context.Context, id store.Identity, docID uuid.UUID, text, comment string) (*ent.Document, error) {
	const maxAppendAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAppendAttempts; attempt++ {
		d, err := s.store.GetDocument(ctx, id, docID)
		if err != nil {
			return nil, err
		}
		newBody := text
		if d.Body != "" {
			newBody = d.Body + "\n" + text
		}
		out, err := s.editLoadedAndIndex(ctx, id, d, store.EditDocument{BaseVersion: d.Version, Body: &newBody, Comment: comment})
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return nil, err
		}
		lastErr = err // a concurrent writer moved the version; re-read and retry
	}
	return nil, lastErr
}

// DeleteSection removes the section under heading (optimistic via base).
func (s *Service) DeleteSection(ctx context.Context, id store.Identity, docID uuid.UUID, base int, heading, comment string) (*ent.Document, error) {
	d, err := s.store.GetDocument(ctx, id, docID)
	if err != nil {
		return nil, err
	}
	newBody, err := docs.DeleteSection(d.Body, heading)
	if err != nil {
		return nil, err
	}
	return s.editLoadedAndIndex(ctx, id, d, store.EditDocument{BaseVersion: base, Body: &newBody, Comment: comment})
}

func (s *Service) RestoreSnapshot(ctx context.Context, id store.Identity, docID uuid.UUID, version, base int, scope store.RestoreScope, comment string) (*ent.Document, error) {
	d, err := s.store.RestoreSnapshot(ctx, id, docID, version, base, scope, comment)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex", d.ID, func() error { return s.index.Reindex(ctx, d.ID) })
	return d, nil
}

func (s *Service) DeleteDocument(ctx context.Context, id store.Identity, docID uuid.UUID) error {
	if err := s.store.DeleteDocument(ctx, id, docID); err != nil {
		return err
	}
	s.syncIndex("remove", docID, func() error { return s.index.Remove(docID) })
	return nil
}

// ---- projects ----

func (s *Service) CreateProject(ctx context.Context, id store.Identity, name, description, visibility string) (*ent.Project, error) {
	return s.store.CreateProject(ctx, id, name, description, visibility)
}

func (s *Service) GetProject(ctx context.Context, id store.Identity, projectID uuid.UUID) (*ent.Project, error) {
	return s.store.GetProject(ctx, id, projectID)
}

func (s *Service) ListProjects(ctx context.Context, id store.Identity, includeArchived bool) ([]*ent.Project, error) {
	return s.store.ListProjects(ctx, id, includeArchived)
}

func (s *Service) ListProjectsWithAccess(ctx context.Context, id store.Identity, includeArchived bool) ([]store.ProjectWithAccess, error) {
	return s.store.ListProjectsWithAccess(ctx, id, includeArchived)
}

func (s *Service) UpdateProject(ctx context.Context, id store.Identity, projectID uuid.UUID, in store.ProjectUpdate) (*ent.Project, error) {
	p, err := s.store.UpdateProject(ctx, id, projectID, in)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return p, nil
}

func (s *Service) ArchiveProject(ctx context.Context, id store.Identity, projectID uuid.UUID) error {
	if err := s.store.ArchiveProject(ctx, id, projectID); err != nil {
		return err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return nil
}

func (s *Service) UnarchiveProject(ctx context.Context, id store.Identity, projectID uuid.UUID) error {
	if err := s.store.UnarchiveProject(ctx, id, projectID); err != nil {
		return err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return nil
}

func (s *Service) DeleteProject(ctx context.Context, id store.Identity, projectID uuid.UUID) error {
	removed, err := s.store.DeleteProject(ctx, id, projectID)
	if err != nil {
		return err
	}
	// Per-doc: log and continue rather than aborting, so one index failure can't strand
	// the remaining evictions. Reconcile-on-read drops any that survive in the index.
	for _, docID := range removed {
		s.syncIndex("remove", docID, func() error { return s.index.Remove(docID) })
	}
	return nil
}

// ---- sharing ----

func (s *Service) ShareUsers(ctx context.Context, id store.Identity, projectID uuid.UUID, emails []string, permission string) (*store.ShareResult, error) {
	res, err := s.store.ShareProjectUsers(ctx, id, projectID, emails, permission)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return res, nil
}

func (s *Service) ShareGroups(ctx context.Context, id store.Identity, projectID uuid.UUID, groups []string, permission string) (*store.ShareResult, error) {
	res, err := s.store.ShareProjectGroups(ctx, id, projectID, groups, permission)
	if err != nil {
		return nil, err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return res, nil
}

func (s *Service) UnshareUsers(ctx context.Context, id store.Identity, projectID uuid.UUID, emails []string) error {
	if err := s.store.UnshareProjectUsers(ctx, id, projectID, emails); err != nil {
		return err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return nil
}

func (s *Service) UnshareGroups(ctx context.Context, id store.Identity, projectID uuid.UUID, groups []string) error {
	if err := s.store.UnshareProjectGroups(ctx, id, projectID, groups); err != nil {
		return err
	}
	s.syncIndex("reindex-project", projectID, func() error { return s.index.ReindexProject(ctx, projectID) })
	return nil
}

func (s *Service) ListShares(ctx context.Context, id store.Identity, projectID uuid.UUID) (*store.ProjectShares, error) {
	return s.store.ListProjectShares(ctx, id, projectID)
}

// ---- search ----

// Search runs a query, stamping the access-scope fields from identity so the agent can
// never widen its own visibility (the tenant/user/group fields are overwritten here, never
// taken from tool input), then reconciles every hit against the live store. A hit the
// caller can no longer read — because the document was deleted, or its project was made
// private / unshared after the index entry was written — is dropped from the results. When
// the document is genuinely gone from the DB (not merely hidden from this caller), the stale
// index entry is best-effort evicted so the index self-heals; an entry that still exists but
// is only inaccessible to this caller is left untouched so other readers keep finding it.
// Result order is preserved.
func (s *Service) Search(ctx context.Context, id store.Identity, q search.Query) ([]search.Result, error) {
	q.TenantID = id.TenantID.String()
	q.UserID = id.UserID.String()
	q.Groups = id.Groups
	hits, err := s.index.Search(q)
	if err != nil {
		return nil, err
	}
	out := make([]search.Result, 0, len(hits))
	for _, h := range hits {
		docID, perr := uuid.Parse(h.DocumentID)
		if perr != nil {
			s.log.Error("search reconcile: unparseable hit id", "id", h.DocumentID, "err", perr)
			continue
		}
		d, gerr := s.store.GetDocument(ctx, id, docID)
		switch {
		case gerr == nil:
			// Refresh display fields from the live doc so a stale-but-accessible entry
			// can't show outdated title/overview.
			h.Title = d.Title
			h.Overview = d.Overview
			out = append(out, h)
		case errors.Is(gerr, store.ErrNotFound):
			// GetDocument returns ErrNotFound both for a deleted document and for one this
			// caller may no longer read (existence is hidden). Either way the hit is dropped
			// for this caller. Only evict from the index when the row is *truly* gone — a
			// tenant-agnostic existence probe distinguishes the two, so we never strip a
			// live doc that other readers can still see.
			if _, ierr := s.store.DocumentForIndex(ctx, docID); errors.Is(ierr, store.ErrNotFound) {
				s.syncIndex("reconcile-remove", docID, func() error { return s.index.Remove(docID) })
			} else if ierr != nil {
				s.log.Error("search reconcile: existence probe failed", "id", h.DocumentID, "err", ierr)
			}
		default:
			s.log.Error("search reconcile: live lookup failed", "id", h.DocumentID, "err", gerr)
		}
	}
	return out, nil
}
