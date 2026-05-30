// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/document"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/project"
)

// withProjectFacts eager-loads the project edges the search indexer needs:
// owner, per-user shares (with user), and group shares.
func withProjectFacts(q *ent.DocumentQuery) *ent.DocumentQuery {
	return q.WithProject(func(pq *ent.ProjectQuery) {
		pq.WithOwner().
			WithShares(func(sq *ent.ProjectShareQuery) { sq.WithUser() }).
			WithGroupShares()
	})
}

// AllDocumentsForIndex returns EVERY document across ALL tenants with project facts
// loaded. Intended only for the trusted indexer (rebuild); never call from a request
// handler — it is deliberately not tenant-scoped.
func (s *Store) AllDocumentsForIndex(ctx context.Context) ([]*ent.Document, error) {
	return withProjectFacts(s.client.Document.Query()).All(ctx)
}

// DocumentForIndex returns one document (any tenant) with project facts loaded.
// Returns ErrNotFound if the document does not exist.
func (s *Store) DocumentForIndex(ctx context.Context, documentID uuid.UUID) (*ent.Document, error) {
	d, err := withProjectFacts(s.client.Document.Query().Where(document.IDEQ(documentID))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return d, err
}

// DocumentsByProjectForIndex returns all documents in a project (any tenant) with
// project facts loaded. Used to re-stamp the index on archive/share changes.
func (s *Store) DocumentsByProjectForIndex(ctx context.Context, projectID uuid.UUID) ([]*ent.Document, error) {
	return withProjectFacts(s.client.Document.Query().Where(document.HasProjectWith(project.IDEQ(projectID)))).All(ctx)
}
