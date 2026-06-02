// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type listProjectsIn struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"include archived projects (default false)"`
}
type listProjectsOut struct {
	Projects []projectOut `json:"projects"`
}
type createProjectIn struct {
	Name        string `json:"name" jsonschema:"project name"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
	Visibility  string `json:"visibility,omitempty" jsonschema:"org (every tenant member can read, edit, and delete) or private (default; owner plus explicit shares)"`
}
type projectIDIn struct {
	ProjectID string `json:"project_id" jsonschema:"the project id"`
}
type updateProjectIn struct {
	ProjectID   string  `json:"project_id" jsonschema:"the project id"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	Visibility  *string `json:"visibility,omitempty" jsonschema:"new visibility: org (every tenant member can read, edit, and delete) or private (owner plus explicit shares)"`
}
type archivedOut struct {
	Archived bool `json:"archived" jsonschema:"the project's archived state after the call"`
}

func (r *registrar) registerProjectTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "list_projects", Description: "List projects you can access.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in listProjectsIn) (*sdk.CallToolResult, listProjectsOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, listProjectsOut{}, err
			}
			ps, err := r.svc.ListProjectsWithAccess(ctx, id, in.IncludeArchived)
			if err != nil {
				return nil, listProjectsOut{}, toolErr(err)
			}
			out := listProjectsOut{Projects: make([]projectOut, 0, len(ps))}
			for _, it := range ps {
				out.Projects = append(out.Projects, toProjectOutWithAccess(it.Project, it.Access.String()))
			}
			return nil, out, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "create_project", Description: "Create a project. Visibility org (every tenant member can read, edit, and delete its documents) or private (default; owner plus explicit shares).", Annotations: mutatingAnno(),
		InputSchema: inputSchema[createProjectIn](map[string][]any{"visibility": {"org", "private"}})},
		func(ctx context.Context, req *sdk.CallToolRequest, in createProjectIn) (*sdk.CallToolResult, projectOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, projectOut{}, err
			}
			vis := in.Visibility
			if vis == "" {
				vis = "private"
			}
			p, err := r.svc.CreateProject(ctx, id, in.Name, in.Description, vis)
			if err != nil {
				return nil, projectOut{}, toolErr(err)
			}
			return nil, toProjectOut(p), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "get_project", Description: "Get a project by id (works for archived projects too).", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in projectIDIn) (*sdk.CallToolResult, projectOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, projectOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, projectOut{}, errInvalidArg("project_id")
			}
			p, err := r.svc.GetProject(ctx, id, pid)
			if err != nil {
				return nil, projectOut{}, toolErr(err)
			}
			return nil, toProjectOut(p), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "update_project", Description: "Update a project's name, description, and/or visibility (owner/admin). Omit a field to leave it unchanged.", Annotations: mutatingAnno(),
		InputSchema: inputSchema[updateProjectIn](map[string][]any{"visibility": {"org", "private"}})},
		func(ctx context.Context, req *sdk.CallToolRequest, in updateProjectIn) (*sdk.CallToolResult, projectOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, projectOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, projectOut{}, errInvalidArg("project_id")
			}
			p, err := r.svc.UpdateProject(ctx, id, pid, store.ProjectUpdate{Name: in.Name, Description: in.Description, Visibility: in.Visibility})
			if err != nil {
				return nil, projectOut{}, toolErr(err)
			}
			return nil, toProjectOut(p), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "archive_project", Description: "Archive a project (owner/admin); hides it from lists and search (reversible).", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in projectIDIn) (*sdk.CallToolResult, archivedOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, archivedOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, archivedOut{}, errInvalidArg("project_id")
			}
			if err := r.svc.ArchiveProject(ctx, id, pid); err != nil {
				return nil, archivedOut{}, toolErr(err)
			}
			return nil, archivedOut{Archived: true}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "unarchive_project", Description: "Unarchive a project (owner/admin); restores it to lists and search.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in projectIDIn) (*sdk.CallToolResult, archivedOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, archivedOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, archivedOut{}, errInvalidArg("project_id")
			}
			if err := r.svc.UnarchiveProject(ctx, id, pid); err != nil {
				return nil, archivedOut{}, toolErr(err)
			}
			return nil, archivedOut{Archived: false}, nil
		})
}
