// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/search"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// ProjectDTO represents a project in JSON.
type ProjectDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Archived    bool   `json:"archived"`
	Access      string `json:"access,omitempty"`
	CanManage   bool   `json:"can_manage"`
}

// toProjectDTO converts an ent.Project to ProjectDTO with the caller's access level and
// management capability.
func toProjectDTO(p *ent.Project, access string, canManage bool) ProjectDTO {
	return ProjectDTO{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Visibility:  p.Visibility.String(),
		Archived:    p.Archived,
		Access:      access,
		CanManage:   canManage,
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

// DocumentDTO represents a document in JSON, carrying both the raw markdown
// body (body) and its server-rendered HTML (body_html).
type DocumentDTO struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id,omitempty"`
	Title         string   `json:"title"`
	Overview      string   `json:"overview"`
	Body          string   `json:"body"`
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
		Body:          d.Body,
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

// SnapshotDTO represents a document snapshot in JSON. BodyHTML is populated only
// by the single get-snapshot endpoint (rendered from the snapshotted markdown);
// list-snapshots returns metadata only and omits the field.
type SnapshotDTO struct {
	Version   int    `json:"version"`
	Comment   string `json:"comment"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	BodyHTML  string `json:"body_html,omitempty"`
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
		Snippet:    sanitizeSnippet(r.Snippet),
	}
}

// ShareDTO is a project's user and group shares (private projects only).
type ShareDTO struct {
	Users  []UserShareDTO  `json:"users"`
	Groups []GroupShareDTO `json:"groups"`
}

type UserShareDTO struct {
	Email      string `json:"email"`
	Permission string `json:"permission"`
}

type GroupShareDTO struct {
	Group      string `json:"group"`
	Permission string `json:"permission"`
}

// toShareDTO converts a store.ProjectShares to ShareDTO, always emitting Users/Groups
// as empty slices rather than null for a stable JSON shape.
func toShareDTO(s *store.ProjectShares) ShareDTO {
	out := ShareDTO{Users: []UserShareDTO{}, Groups: []GroupShareDTO{}}
	for _, u := range s.Users {
		out.Users = append(out.Users, UserShareDTO{Email: u.Email, Permission: u.Permission})
	}
	for _, g := range s.Groups {
		out.Groups = append(out.Groups, GroupShareDTO{Group: g.Group, Permission: g.Permission})
	}
	return out
}
