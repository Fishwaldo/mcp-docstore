// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/search"
)

type searchDocumentsIn struct {
	Query      string   `json:"query" jsonschema:"plain keywords/phrase to search for"`
	ProjectID  string   `json:"project_id,omitempty" jsonschema:"optional project filter"`
	Visibility string   `json:"visibility,omitempty" jsonschema:"optional visibility filter: org|private"`
	Tags       []string `json:"tags,omitempty" jsonschema:"optional tag filter (all must match)"`
	Limit      int      `json:"limit,omitempty" jsonschema:"max results (default 20)"`
}
type searchDocumentsOut struct {
	Results []searchHitOut `json:"results"`
}

func (r *registrar) registerSearchTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "search_documents", Description: "Full-text search across documents the caller can read. Returns ranked hits with snippets.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in searchDocumentsIn) (*sdk.CallToolResult, searchDocumentsOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, searchDocumentsOut{}, err
			}
			q := search.Query{
				Text:       in.Query,
				ProjectID:  in.ProjectID,
				Visibility: in.Visibility,
				Tags:       in.Tags,
				Limit:      in.Limit,
			}
			hits, err := r.svc.Search(id, q)
			if err != nil {
				return nil, searchDocumentsOut{}, toolErr(err)
			}
			out := searchDocumentsOut{Results: make([]searchHitOut, 0, len(hits))}
			for _, h := range hits {
				out.Results = append(out.Results, searchHitOut{
					DocumentID: h.DocumentID,
					ProjectID:  h.ProjectID,
					Title:      h.Title,
					Overview:   h.Overview,
					Score:      h.Score,
					Snippet:    h.Snippet,
				})
			}
			return nil, out, nil
		})
}
