// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type deleteDocumentIn struct {
	DocumentID string `json:"document_id" jsonschema:"the document to delete"`
	Confirm    bool   `json:"confirm,omitempty" jsonschema:"set true to confirm when the client cannot prompt interactively"`
}
type deleteProjectIn struct {
	ProjectID string `json:"project_id" jsonschema:"the project to delete"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"set true to confirm when the client cannot prompt interactively"`
}
type restoreSnapshotIn struct {
	DocumentID  string `json:"document_id" jsonschema:"the document id"`
	Version     int    `json:"version" jsonschema:"the snapshot version to restore"`
	BaseVersion int    `json:"base_version" jsonschema:"current version you read; rejected if stale"`
	Scope       string `json:"scope,omitempty" jsonschema:"what to restore: body (default) restores only the body and preserves the live overview and tags; full also restores the snapshot's overview and tags"`
	Comment     string `json:"comment,omitempty" jsonschema:"optional change comment"`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"set true to confirm when the client cannot prompt interactively"`
}
type deletedOut struct {
	Deleted bool `json:"deleted" jsonschema:"true once the item has been permanently deleted"`
}

func (r *registrar) registerDestructiveTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "delete_document", Description: "Delete a document. Destructive; confirmation required.", Annotations: destructiveAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in deleteDocumentIn) (*sdk.CallToolResult, deletedOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, deletedOut{}, err
			}
			docID, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, deletedOut{}, errInvalidArg("document_id")
			}
			if err := r.svc.EnsureDocumentWritable(ctx, id, docID); err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			d, err := r.svc.GetDocument(ctx, id, docID)
			if err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			if err := confirm(ctx, req, fmt.Sprintf("Delete document %q? This cannot be undone.", d.Title), in.Confirm); err != nil {
				return nil, deletedOut{}, err
			}
			if err := r.svc.DeleteDocument(ctx, id, docID); err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			return nil, deletedOut{Deleted: true}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "delete_project", Description: "Delete a project and all its documents. Destructive; confirmation required.", Annotations: destructiveAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in deleteProjectIn) (*sdk.CallToolResult, deletedOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, deletedOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, deletedOut{}, errInvalidArg("project_id")
			}
			if err := r.svc.EnsureProjectOwner(ctx, id, pid); err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			p, err := r.svc.GetProject(ctx, id, pid)
			if err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			docs, err := r.svc.ListDocuments(ctx, id, pid)
			if err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			if err := confirm(ctx, req, fmt.Sprintf("Delete project %q and its %d document(s)? This cannot be undone.", p.Name, len(docs)), in.Confirm); err != nil {
				return nil, deletedOut{}, err
			}
			if err := r.svc.DeleteProject(ctx, id, pid); err != nil {
				return nil, deletedOut{}, toolErr(err)
			}
			return nil, deletedOut{Deleted: true}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "restore_snapshot",
		Description: "Restore a previous version of a document. By default (scope=body) only the body is restored and the live overview and tags are preserved; pass scope=full to also restore the snapshot's overview and tags. Destructive; confirmation required.",
		Annotations: destructiveAnno(),
		InputSchema: inputSchema[restoreSnapshotIn](map[string][]any{"scope": {"body", "full"}}),
	},
		func(ctx context.Context, req *sdk.CallToolRequest, in restoreSnapshotIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			docID, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("document_id")
			}
			if err := r.svc.EnsureDocumentWritable(ctx, id, docID); err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			scope := store.RestoreScopeBody
			if in.Scope == string(store.RestoreScopeFull) {
				scope = store.RestoreScopeFull
			}
			snap, err := r.svc.GetSnapshot(ctx, id, docID, in.Version)
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			if err := confirm(ctx, req, fmt.Sprintf("Restore version %d (%q) over the current content (scope=%s)?", in.Version, snap.Comment, scope), in.Confirm); err != nil {
				return nil, documentOut{}, err
			}
			d, err := r.svc.RestoreSnapshot(ctx, id, docID, in.Version, in.BaseVersion, scope, in.Comment)
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d, r.webBaseURL), nil
		})
}
