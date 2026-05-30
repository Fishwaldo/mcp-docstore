// Package index keeps the Bleve search index in sync with the store. It is the only
// package that knows both ent (via store) and search, and owns the ent→search.Doc mapping.
package index

import (
	"context"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// Service syncs documents from the store into the search index.
type Service struct {
	store *store.Store
	index *search.Index
}

// New constructs a Service bridging the store and the search index.
func New(s *store.Store, idx *search.Index) *Service {
	return &Service{store: s, index: idx}
}

// toDoc maps an ent.Document (with project facts eager-loaded) to a search.Doc.
func toDoc(d *ent.Document) search.Doc {
	doc := search.Doc{
		ID:       d.ID.String(),
		TenantID: d.TenantID.String(),
		Title:    d.Title,
		Overview: d.Overview,
		Body:     d.Body,
		Tags:     d.Tags,
	}
	if p := d.Edges.Project; p != nil {
		doc.ProjectID = p.ID.String()
		doc.Visibility = p.Visibility.String()
		doc.ProjectArchived = p.Archived
		if p.Edges.Owner != nil {
			doc.OwnerID = p.Edges.Owner.ID.String()
		}
		for _, sh := range p.Edges.Shares {
			if sh.Edges.User != nil {
				doc.SharedUserIDs = append(doc.SharedUserIDs, sh.Edges.User.ID.String())
			}
		}
		for _, gs := range p.Edges.GroupShares {
			doc.SharedGroups = append(doc.SharedGroups, gs.GroupName)
		}
	}
	return doc
}

// Search runs a query against the index. The mcp layer builds the access-scoped Query
// from the authenticated identity; this is a thin passthrough that keeps search wiring
// out of the mcp package.
func (s *Service) Search(q search.Query) ([]search.Result, error) {
	return s.index.Search(q)
}

// Reindex (re)indexes a single document by ID.
func (s *Service) Reindex(ctx context.Context, documentID uuid.UUID) error {
	d, err := s.store.DocumentForIndex(ctx, documentID)
	if err != nil {
		return err
	}
	return s.index.Put(toDoc(d))
}

// Remove deletes a document from the index.
func (s *Service) Remove(documentID uuid.UUID) error {
	return s.index.Delete(documentID.String())
}

// ReindexProject re-stamps every document of a project (used after archive/unarchive
// or share changes, which alter indexed access/archived facts).
func (s *Service) ReindexProject(ctx context.Context, projectID uuid.UUID) error {
	docs, err := s.store.DocumentsByProjectForIndex(ctx, projectID)
	if err != nil {
		return err
	}
	for _, d := range docs {
		if err := s.index.Put(toDoc(d)); err != nil {
			return err
		}
	}
	return nil
}

// RebuildAll rebuilds the entire index from the DB. It first Resets (drops) the index
// so stale entries — documents deleted from the DB, or leftovers from an old mapping —
// are cleared, then reindexes every document across all tenants. Used at boot when the
// index path is empty and by the CLI `rebuild-index` subcommand (Phase 4).
func (s *Service) RebuildAll(ctx context.Context) error {
	docs, err := s.store.AllDocumentsForIndex(ctx)
	if err != nil {
		return err
	}
	// Read the DB first, then clear the index, so a transient DB error doesn't leave
	// the index empty.
	if err := s.index.Reset(); err != nil {
		return err
	}
	for _, d := range docs {
		if err := s.index.Put(toDoc(d)); err != nil {
			return err
		}
	}
	return nil
}
