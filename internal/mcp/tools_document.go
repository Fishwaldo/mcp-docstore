// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type listDocumentsOut struct {
	Documents []documentSummaryOut `json:"documents"`
}
type createDocumentIn struct {
	ProjectID string   `json:"project_id" jsonschema:"the project id"`
	Title     string   `json:"title" jsonschema:"document title"`
	Overview  string   `json:"overview,omitempty" jsonschema:"short overview/summary"`
	Body      string   `json:"body,omitempty" jsonschema:"document body (markdown)"`
	Tags      []string `json:"tags,omitempty" jsonschema:"tags"`
	Comment   string   `json:"comment,omitempty" jsonschema:"optional change comment"`
}
type documentIDIn struct {
	DocumentID string `json:"document_id" jsonschema:"the document id"`
}
type editDocumentIn struct {
	DocumentID  string    `json:"document_id" jsonschema:"the document id"`
	BaseVersion int       `json:"base_version" jsonschema:"version you read; rejected if stale"`
	Mode        string    `json:"mode" jsonschema:"replace or section"`
	Comment     string    `json:"comment,omitempty" jsonschema:"optional change comment"`
	Overview    *string   `json:"overview,omitempty" jsonschema:"replace mode: new overview"`
	Body        *string   `json:"body,omitempty" jsonschema:"replace mode: new body"`
	Tags        *[]string `json:"tags,omitempty" jsonschema:"replace mode: new tags"`
	Heading     string    `json:"heading,omitempty" jsonschema:"section mode: heading to replace"`
	Content     string    `json:"content,omitempty" jsonschema:"section mode: new section content"`
}

func (r *registrar) registerDocumentTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "list_documents", Description: "List documents in a project.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in projectIDIn) (*sdk.CallToolResult, listDocumentsOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, listDocumentsOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, listDocumentsOut{}, errInvalidArg("project_id")
			}
			ds, err := r.svc.ListDocuments(ctx, id, pid)
			if err != nil {
				return nil, listDocumentsOut{}, toolErr(err)
			}
			out := listDocumentsOut{Documents: make([]documentSummaryOut, 0, len(ds))}
			for _, d := range ds {
				out.Documents = append(out.Documents, toDocumentSummary(d))
			}
			return nil, out, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "create_document", Description: "Create a document in a project.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in createDocumentIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("project_id")
			}
			d, err := r.svc.CreateDocument(ctx, id, pid, store.NewDocument{
				Title: in.Title, Overview: in.Overview, Body: in.Body, Tags: in.Tags, Comment: in.Comment,
			})
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "get_document", Description: "Get a document by id, including its full body and current version.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in documentIDIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("document_id")
			}
			d, err := r.svc.GetDocument(ctx, id, did)
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "edit_document", Description: "Edit a document. mode=replace updates overview/body/tags wholesale; mode=section replaces one markdown section by heading. base_version must match the current version or the edit is rejected.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in editDocumentIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("document_id")
			}
			var d *ent.Document
			switch in.Mode {
			case "replace":
				if in.Overview == nil && in.Body == nil && in.Tags == nil {
					return nil, documentOut{}, fmt.Errorf("%w: replace requires at least one of overview/body/tags", store.ErrInvalid)
				}
				d, err = r.svc.EditReplace(ctx, id, did, in.BaseVersion, in.Overview, in.Body, in.Tags, in.Comment)
			case "section":
				if in.Heading == "" || in.Content == "" {
					return nil, documentOut{}, fmt.Errorf("%w: section mode requires heading and content", store.ErrInvalid)
				}
				d, err = r.svc.EditSection(ctx, id, did, in.BaseVersion, in.Heading, in.Content, in.Comment)
			default:
				return nil, documentOut{}, fmt.Errorf("%w: mode must be replace or section", store.ErrInvalid)
			}
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d), nil
		})
}
