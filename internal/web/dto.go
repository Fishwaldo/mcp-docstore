// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
)

// ProjectDTO represents a project in JSON.
type ProjectDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Archived    bool   `json:"archived"`
	Access      string `json:"access,omitempty"`
}

// toProjectDTO converts an ent.Project to ProjectDTO with the caller's access level.
func toProjectDTO(p *ent.Project, access string) ProjectDTO {
	return ProjectDTO{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Visibility:  p.Visibility.String(),
		Archived:    p.Archived,
		Access:      access,
	}
}

// DocumentSummaryDTO represents a document summary (overview only) in JSON.
type DocumentSummaryDTO struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Overview  string   `json:"overview"`
	Tags      []string `json:"tags"`
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
}

// toDocumentSummary converts an ent.Document to DocumentSummaryDTO.
func toDocumentSummary(d *ent.Document) DocumentSummaryDTO {
	return DocumentSummaryDTO{
		ID:        d.ID.String(),
		Title:     d.Title,
		Overview:  d.Overview,
		Tags:      d.Tags,
		Version:   d.Version,
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
	}
}

// DocumentDTO represents a document with rendered HTML body in JSON.
type DocumentDTO struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id,omitempty"`
	Title         string   `json:"title"`
	Overview      string   `json:"overview"`
	Tags          []string `json:"tags"`
	Version       int      `json:"version"`
	ChangeComment string   `json:"change_comment"`
	UpdatedAt     string   `json:"updated_at"`
	BodyHTML      string   `json:"body_html"`
}

// toDocumentDTO converts an ent.Document and rendered HTML to DocumentDTO.
func toDocumentDTO(d *ent.Document, bodyHTML string) DocumentDTO {
	out := DocumentDTO{
		ID:            d.ID.String(),
		Title:         d.Title,
		Overview:      d.Overview,
		Tags:          d.Tags,
		Version:       d.Version,
		ChangeComment: d.ChangeComment,
		UpdatedAt:     d.UpdatedAt.Format(time.RFC3339),
		BodyHTML:      bodyHTML,
	}
	if d.Edges.Project != nil {
		out.ProjectID = d.Edges.Project.ID.String()
	}
	return out
}

// SnapshotDTO represents a document snapshot's metadata in JSON.
type SnapshotDTO struct {
	Version   int    `json:"version"`
	Comment   string `json:"comment"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

// toSnapshotDTO converts an ent.DocumentSnapshot to SnapshotDTO.
func toSnapshotDTO(s *ent.DocumentSnapshot) SnapshotDTO {
	out := SnapshotDTO{
		Version:   s.Version,
		Comment:   s.Comment,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
	}
	if u := s.Edges.CreatedBy; u != nil {
		if u.Email != "" {
			out.CreatedBy = u.Email
		} else {
			out.CreatedBy = u.ID.String()
		}
	}
	return out
}

// SearchHitDTO represents a search result in JSON.
type SearchHitDTO struct {
	DocumentID string  `json:"document_id"`
	ProjectID  string  `json:"project_id"`
	Title      string  `json:"title"`
	Overview   string  `json:"overview"`
	Score      float64 `json:"score"`
	Snippet    string  `json:"snippet"`
}

// toSearchHit converts a search.Result to SearchHitDTO.
func toSearchHit(r search.Result) SearchHitDTO {
	return SearchHitDTO{
		DocumentID: r.DocumentID,
		ProjectID:  r.ProjectID,
		Title:      r.Title,
		Overview:   r.Overview,
		Score:      r.Score,
		Snippet:    r.Snippet,
	}
}
