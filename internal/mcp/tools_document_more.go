// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type getSectionIn struct {
	DocumentID string `json:"document_id" jsonschema:"the document id"`
	Heading    string `json:"heading" jsonschema:"the markdown heading to fetch"`
}
type getSectionOut struct {
	Content string `json:"content"`
}
type appendDocumentIn struct {
	DocumentID string `json:"document_id" jsonschema:"the document id"`
	Text       string `json:"text" jsonschema:"text to append to the body"`
	Comment    string `json:"comment,omitempty" jsonschema:"optional change comment"`
}
type deleteSectionIn struct {
	DocumentID  string `json:"document_id" jsonschema:"the document id"`
	BaseVersion int    `json:"base_version" jsonschema:"version you read; rejected if stale"`
	Heading     string `json:"heading" jsonschema:"the markdown heading to delete"`
	Comment     string `json:"comment,omitempty" jsonschema:"optional change comment"`
}

func (r *registrar) registerDocumentMoreTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "get_section", Description: "Get the content of a single markdown section by heading.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in getSectionIn) (*sdk.CallToolResult, getSectionOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, getSectionOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, getSectionOut{}, errInvalidArg("document_id")
			}
			content, err := r.svc.GetSection(ctx, id, did, in.Heading)
			if err != nil {
				return nil, getSectionOut{}, toolErr(err)
			}
			return nil, getSectionOut{Content: content}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "append_document", Description: "Append text to a document body. Non-clobbering: no base_version required.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in appendDocumentIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("document_id")
			}
			d, err := r.svc.AppendDocument(ctx, id, did, in.Text, in.Comment)
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "delete_section", Description: "Delete a markdown section by heading. base_version must match the current version or the edit is rejected.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in deleteSectionIn) (*sdk.CallToolResult, documentOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, documentOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, documentOut{}, errInvalidArg("document_id")
			}
			d, err := r.svc.DeleteSection(ctx, id, did, in.BaseVersion, in.Heading, in.Comment)
			if err != nil {
				return nil, documentOut{}, toolErr(err)
			}
			return nil, toDocumentOut(d), nil
		})
}
