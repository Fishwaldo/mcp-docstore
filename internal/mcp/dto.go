// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"time"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

type projectOut struct {
	ID          string `json:"id" jsonschema:"project id (UUID); pass as project_id to other tools"`
	Name        string `json:"name" jsonschema:"project name"`
	Description string `json:"description" jsonschema:"project description"`
	Visibility  string `json:"visibility" jsonschema:"org (every tenant member can read, edit, and delete its documents) or private (owner plus explicit shares)"`
	Archived    bool   `json:"archived" jsonschema:"true if archived (hidden from lists and search)"`
	Access      string `json:"access,omitempty" jsonschema:"the caller's effective access to this project: none, read, or write"`
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
	ID            string   `json:"id" jsonschema:"document id (UUID); pass as document_id to other tools"`
	ProjectID     string   `json:"project_id,omitempty" jsonschema:"id of the project that contains this document"`
	Title         string   `json:"title" jsonschema:"document title"`
	Overview      string   `json:"overview" jsonschema:"short summary; scan this before fetching or searching full bodies"`
	Body          string   `json:"body" jsonschema:"full markdown body"`
	Tags          []string `json:"tags" jsonschema:"tags"`
	Version       int      `json:"version" jsonschema:"current version; pass as base_version on the next edit so a stale write is rejected"`
	ChangeComment string   `json:"change_comment" jsonschema:"comment recorded with the change that produced this version"`
	UpdatedAt     string   `json:"updated_at" jsonschema:"last-modified time (RFC 3339)"`
	WebURL        string   `json:"web_url,omitempty" jsonschema:"URL to view this document in the web UI (present only when the web UI is enabled)"`
}

func toDocumentOut(d *ent.Document, webBaseURL string) documentOut {
	out := documentOut{
		ID: d.ID.String(), Title: d.Title, Overview: d.Overview, Body: d.Body,
		Tags: d.Tags, Version: d.Version, ChangeComment: d.ChangeComment,
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
	}
	if d.Edges.Project != nil {
		out.ProjectID = d.Edges.Project.ID.String()
	}
	if webBaseURL != "" {
		out.WebURL = webBaseURL + "/documents/" + d.ID.String()
	}
	return out
}

type documentSummaryOut struct {
	ID        string   `json:"id" jsonschema:"document id (UUID); pass as document_id to other tools"`
	Title     string   `json:"title" jsonschema:"document title"`
	Overview  string   `json:"overview" jsonschema:"short summary (no body); use get_document for the full body"`
	Tags      []string `json:"tags" jsonschema:"tags"`
	Version   int      `json:"version" jsonschema:"current version; pass as base_version when editing"`
	UpdatedAt string   `json:"updated_at" jsonschema:"last-modified time (RFC 3339)"`
	WebURL    string   `json:"web_url,omitempty" jsonschema:"URL to view this document in the web UI (present only when the web UI is enabled)"`
}

func toDocumentSummary(d *ent.Document, webBaseURL string) documentSummaryOut {
	out := documentSummaryOut{ID: d.ID.String(), Title: d.Title, Overview: d.Overview, Tags: d.Tags, Version: d.Version, UpdatedAt: d.UpdatedAt.Format(time.RFC3339)}
	if webBaseURL != "" {
		out.WebURL = webBaseURL + "/documents/" + d.ID.String()
	}
	return out
}

type snapshotMetaOut struct {
	Version   int    `json:"version" jsonschema:"the snapshot's version number; pass as version to get_snapshot/restore_snapshot"`
	Comment   string `json:"comment" jsonschema:"change comment recorded for this version"`
	CreatedBy string `json:"created_by" jsonschema:"email (or id) of the user who created this version"`
	CreatedAt string `json:"created_at" jsonschema:"time the version was created (RFC 3339)"`
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
	Version  int      `json:"version" jsonschema:"the snapshot's version number"`
	Overview string   `json:"overview" jsonschema:"the document overview at this version"`
	Body     string   `json:"body" jsonschema:"the full markdown body at this version"`
	Tags     []string `json:"tags" jsonschema:"the document tags at this version"`
	Comment  string   `json:"comment" jsonschema:"change comment recorded for this version"`
}

func toSnapshotOut(s *ent.DocumentSnapshot) snapshotOut {
	return snapshotOut{Version: s.Version, Overview: s.Overview, Body: s.Body, Tags: s.Tags, Comment: s.Comment}
}

type searchHitOut struct {
	DocumentID string  `json:"document_id" jsonschema:"matching document id; pass as document_id to get_document"`
	ProjectID  string  `json:"project_id" jsonschema:"id of the project that contains the document"`
	Title      string  `json:"title" jsonschema:"document title"`
	Overview   string  `json:"overview" jsonschema:"document overview"`
	Score      float64 `json:"score" jsonschema:"relevance score; higher is a better match, comparable only within this result set"`
	Snippet    string  `json:"snippet" jsonschema:"excerpt of the body around the match"`
	WebURL     string  `json:"web_url,omitempty" jsonschema:"URL to view this document in the web UI (present only when the web UI is enabled)"`
}

type shareUserOut struct {
	Email      string `json:"email" jsonschema:"the shared user's email"`
	Permission string `json:"permission" jsonschema:"read or write"`
}
type shareGroupOut struct {
	Group      string `json:"group" jsonschema:"the shared group name"`
	Permission string `json:"permission" jsonschema:"read or write"`
}
