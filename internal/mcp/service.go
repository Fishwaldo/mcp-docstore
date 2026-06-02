// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package mcp wires the store, search index, and goldmark editing into the MCP tool
// surface. The Service bundles each store mutation with its search-index sync and any
// markdown orchestration; all methods take a resolved store.Identity.
package mcp

import (
	"context"
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
	return s.EditReplace(ctx, id, docID, base, nil, &newBody, nil, comment)
}

// AppendDocument appends text to the end of the body. Non-clobbering: it does
// NOT take a caller base_version; it reads the current version and uses it as the base, so
// concurrent appends never conflict. Still produces a new version + snapshot.
func (s *Service) AppendDocument(ctx context.Context, id store.Identity, docID uuid.UUID, text, comment string) (*ent.Document, error) {
	d, err := s.store.GetDocument(ctx, id, docID)
	if err != nil {
		return nil, err
	}
	newBody := text
	if d.Body != "" {
		newBody = d.Body + "\n" + text
	}
	return s.EditReplace(ctx, id, docID, d.Version, nil, &newBody, nil, comment)
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
	return s.EditReplace(ctx, id, docID, base, nil, &newBody, nil, comment)
}

func (s *Service) RestoreSnapshot(ctx context.Context, id store.Identity, docID uuid.UUID, version, base int, comment string) (*ent.Document, error) {
	d, err := s.store.RestoreSnapshot(ctx, id, docID, version, base, comment)
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
		docID := docID
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
// never widen its own visibility (the tenant/user/group fields are overwritten here,
// never taken from tool input).
func (s *Service) Search(id store.Identity, q search.Query) ([]search.Result, error) {
	q.TenantID = id.TenantID.String()
	q.UserID = id.UserID.String()
	q.Groups = id.Groups
	return s.index.Search(q)
}
