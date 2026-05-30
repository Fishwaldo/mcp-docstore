// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

type projectOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Archived    bool   `json:"archived"`
	Access      string `json:"access,omitempty"`
}

func toProjectOut(p *ent.Project) projectOut {
	return projectOut{ID: p.ID.String(), Name: p.Name, Description: p.Description, Visibility: p.Visibility.String(), Archived: p.Archived}
}

// toProjectOutWithAccess is toProjectOut plus the caller's effective access level.
func toProjectOutWithAccess(p *ent.Project, access string) projectOut {
	o := toProjectOut(p)
	o.Access = access
	return o
}

type documentOut struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id,omitempty"`
	Title         string   `json:"title"`
	Overview      string   `json:"overview"`
	Body          string   `json:"body"`
	Tags          []string `json:"tags"`
	Version       int      `json:"version"`
	ChangeComment string   `json:"change_comment"`
	UpdatedAt     string   `json:"updated_at"`
}

func toDocumentOut(d *ent.Document) documentOut {
	out := documentOut{
		ID: d.ID.String(), Title: d.Title, Overview: d.Overview, Body: d.Body,
		Tags: d.Tags, Version: d.Version, ChangeComment: d.ChangeComment,
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
	}
	if d.Edges.Project != nil {
		out.ProjectID = d.Edges.Project.ID.String()
	}
	return out
}

type documentSummaryOut struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Overview  string   `json:"overview"`
	Tags      []string `json:"tags"`
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
}

func toDocumentSummary(d *ent.Document) documentSummaryOut {
	return documentSummaryOut{ID: d.ID.String(), Title: d.Title, Overview: d.Overview, Tags: d.Tags, Version: d.Version, UpdatedAt: d.UpdatedAt.Format(time.RFC3339)}
}

type snapshotMetaOut struct {
	Version   int    `json:"version"`
	Comment   string `json:"comment"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func toSnapshotMeta(s *ent.DocumentSnapshot) snapshotMetaOut {
	out := snapshotMetaOut{Version: s.Version, Comment: s.Comment, CreatedAt: s.CreatedAt.Format(time.RFC3339)}
	if u := s.Edges.CreatedBy; u != nil {
		if u.Email != "" {
			out.CreatedBy = u.Email
		} else {
			out.CreatedBy = u.ID.String()
		}
	}
	return out
}

type snapshotOut struct {
	Version  int      `json:"version"`
	Overview string   `json:"overview"`
	Body     string   `json:"body"`
	Tags     []string `json:"tags"`
	Comment  string   `json:"comment"`
}

func toSnapshotOut(s *ent.DocumentSnapshot) snapshotOut {
	return snapshotOut{Version: s.Version, Overview: s.Overview, Body: s.Body, Tags: s.Tags, Comment: s.Comment}
}

type searchHitOut struct {
	DocumentID string  `json:"document_id"`
	ProjectID  string  `json:"project_id"`
	Title      string  `json:"title"`
	Overview   string  `json:"overview"`
	Score      float64 `json:"score"`
	Snippet    string  `json:"snippet"`
}

type shareUserOut struct {
	Email      string `json:"email"`
	Permission string `json:"permission"`
}
type shareGroupOut struct {
	Group      string `json:"group"`
	Permission string `json:"permission"`
}
