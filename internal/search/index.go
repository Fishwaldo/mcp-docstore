// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package search

import (
	"fmt"
	"os"

	bleve "github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Index wraps a persistent Bleve index.
type Index struct {
	idx    bleve.Index
	path   string
	closed bool
}

// Open opens the index at path, creating it (with our mapping) if it does not exist.
// The index is persistent: existing data is reused, never rebuilt on open.
func Open(path string) (*Index, error) {
	idx, err := bleve.Open(path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		idx, err = bleve.New(path, buildMapping())
	}
	if err != nil {
		return nil, fmt.Errorf("open search index: %w", err)
	}
	return &Index{idx: idx, path: path}, nil
}

// Close closes the underlying Bleve index. It is idempotent: a second call is a no-op,
// since Bleve's scorch backend panics on a double close.
func (i *Index) Close() error {
	if i.closed {
		return nil
	}
	i.closed = true
	return i.idx.Close()
}

// Reset drops the entire index and recreates it empty with the current mapping. It
// clears any stale entries (e.g. documents deleted from the DB, or leftovers from an
// older mapping) so a subsequent full reindex starts clean. Used by RebuildAll.
func (i *Index) Reset() error {
	if err := i.idx.Close(); err != nil {
		return fmt.Errorf("close before reset: %w", err)
	}
	if err := os.RemoveAll(i.path); err != nil {
		return fmt.Errorf("remove index path: %w", err)
	}
	idx, err := bleve.New(i.path, buildMapping())
	if err != nil {
		return fmt.Errorf("recreate index: %w", err)
	}
	i.idx = idx
	return nil
}

// Put indexes (or replaces) a document by its ID.
func (i *Index) Put(d Doc) error {
	if err := i.idx.Index(d.ID, d); err != nil {
		return fmt.Errorf("index doc %s: %w", d.ID, err)
	}
	return nil
}

// Delete removes a document from the index (no error if absent).
func (i *Index) Delete(id string) error {
	if err := i.idx.Delete(id); err != nil {
		return fmt.Errorf("delete doc %s: %w", id, err)
	}
	return nil
}

func (i *Index) count() (uint64, error) { return i.idx.DocCount() }

// IsEmpty reports whether the index currently holds zero documents. Used at boot to
// decide whether to rebuild from the DB.
func (i *Index) IsEmpty() (bool, error) {
	n, err := i.count()
	return n == 0, err
}

// Search runs an access-scoped query. The caller-supplied access fields (TenantID,
// UserID, Groups) build mandatory filter clauses that the Text cannot override.
func (i *Index) Search(q Query) ([]Result, error) {
	must := []query.Query{}

	// (1) tenant scope — exact term.
	tenantQ := bleve.NewTermQuery(q.TenantID)
	tenantQ.SetField("tenant_id")
	must = append(must, tenantQ)

	// (2) archived projects excluded.
	notArchived := bleve.NewBoolFieldQuery(false)
	notArchived.SetField("project_archived")
	must = append(must, notArchived)

	// (3) text match across title/overview/body, or match-all when empty.
	if q.Text == "" {
		must = append(must, bleve.NewMatchAllQuery())
	} else {
		must = append(must, textDisjunction(q.Text))
	}

	// (4) access disjunction (>=1 must hold).
	must = append(must, accessDisjunction(q))

	// optional structured filters.
	if q.ProjectID != "" {
		pq := bleve.NewTermQuery(q.ProjectID)
		pq.SetField("project_id")
		must = append(must, pq)
	}
	if q.Visibility != "" {
		vq := bleve.NewTermQuery(q.Visibility)
		vq.SetField("visibility")
		must = append(must, vq)
	}
	for _, tag := range q.Tags {
		tq := bleve.NewTermQuery(tag)
		tq.SetField("tags")
		must = append(must, tq)
	}

	boolQ := bleve.NewBooleanQuery()
	boolQ.AddMust(must...)

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	const maxLimit = 100
	if limit > maxLimit {
		limit = maxLimit
	}
	req := bleve.NewSearchRequestOptions(boolQ, limit, 0, false)
	req.Fields = []string{"title", "overview", "project_id"}
	// Highlight only the human-readable fields. Without this restriction Bleve also
	// highlights the identity fields matched by accessDisjunction (owner_id,
	// shared_user_ids), so the snippet would echo the caller's own UUID.
	req.Highlight = bleve.NewHighlight()
	req.Highlight.Fields = []string{"title", "overview", "body"}

	sr, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	out := make([]Result, 0, len(sr.Hits))
	for _, hit := range sr.Hits {
		r := Result{
			DocumentID: hit.ID,
			Score:      hit.Score,
			Title:      stringField(hit.Fields, "title"),
			Overview:   stringField(hit.Fields, "overview"),
			ProjectID:  stringField(hit.Fields, "project_id"),
		}
		// Pick the fragment by a fixed field priority; hit.Fragments is a map, so
		// iterating it directly would choose nondeterministically across calls.
		for _, f := range []string{"body", "overview", "title"} {
			if frags := hit.Fragments[f]; len(frags) > 0 {
				r.Snippet = frags[0]
				break
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func textDisjunction(text string) query.Query {
	fields := []string{"title", "overview", "body"}
	qs := make([]query.Query, 0, len(fields))
	for _, f := range fields {
		mq := bleve.NewMatchQuery(text)
		mq.SetField(f)
		qs = append(qs, mq)
	}
	dq := bleve.NewDisjunctionQuery(qs...)
	dq.SetMin(1)
	return dq
}

func accessDisjunction(q Query) query.Query {
	clauses := []query.Query{}

	// Guard against an empty UserID: an unguarded TermQuery("") on owner_id would
	// match any document with an empty/missing owner_id, granting access to an
	// unidentified caller. Fail closed — only add identity clauses for a real user.
	if q.UserID != "" {
		owner := bleve.NewTermQuery(q.UserID)
		owner.SetField("owner_id")
		clauses = append(clauses, owner)

		us := bleve.NewTermQuery(q.UserID)
		us.SetField("shared_user_ids")
		clauses = append(clauses, us)
	}

	org := bleve.NewTermQuery("org")
	org.SetField("visibility")
	clauses = append(clauses, org)
	for _, g := range q.Groups {
		gs := bleve.NewTermQuery(g)
		gs.SetField("shared_groups")
		clauses = append(clauses, gs)
	}
	dq := bleve.NewDisjunctionQuery(clauses...)
	dq.SetMin(1)
	return dq
}

func stringField(fields map[string]interface{}, name string) string {
	if v, ok := fields[name].(string); ok {
		return v
	}
	return ""
}

// buildMapping configures keyword (exact-match) fields for all filter/identity fields,
// english-analyzed text for title/overview/body, and a boolean for project_archived.
func buildMapping() mapping.IndexMapping {
	kw := bleve.NewKeywordFieldMapping()

	text := bleve.NewTextFieldMapping()
	text.Analyzer = "en"
	text.Store = true

	stored := bleve.NewKeywordFieldMapping()
	stored.Store = true

	boolean := bleve.NewBooleanFieldMapping()

	doc := bleve.NewDocumentMapping()
	doc.AddFieldMappingsAt("tenant_id", kw)
	doc.AddFieldMappingsAt("project_id", stored) // exact-match AND stored for results
	doc.AddFieldMappingsAt("owner_id", kw)
	doc.AddFieldMappingsAt("visibility", kw)
	doc.AddFieldMappingsAt("shared_user_ids", kw)
	doc.AddFieldMappingsAt("shared_groups", kw)
	doc.AddFieldMappingsAt("tags", kw)
	doc.AddFieldMappingsAt("project_archived", boolean)
	doc.AddFieldMappingsAt("title", text)
	doc.AddFieldMappingsAt("overview", text)
	doc.AddFieldMappingsAt("body", text)

	im := bleve.NewIndexMapping()
	im.DefaultMapping = doc
	im.DefaultAnalyzer = "en"
	return im
}
