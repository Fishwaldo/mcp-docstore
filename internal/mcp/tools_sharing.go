// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

type shareProjectIn struct {
	ProjectID  string   `json:"project_id" jsonschema:"the project id"`
	Users      []string `json:"users,omitempty" jsonschema:"user emails to share with"`
	Groups     []string `json:"groups,omitempty" jsonschema:"group names to share with"`
	Permission string   `json:"permission,omitempty" jsonschema:"read or write (default read)"`
}
type shareProjectOut struct {
	Unresolved []string `json:"unresolved" jsonschema:"emails that matched no tenant member and were skipped (not shared)"`
}
type unshareProjectIn struct {
	ProjectID string   `json:"project_id" jsonschema:"the project id"`
	Users     []string `json:"users,omitempty" jsonschema:"user emails to unshare"`
	Groups    []string `json:"groups,omitempty" jsonschema:"group names to unshare"`
}
type unshareProjectOut struct {
	Removed bool `json:"removed"`
}
type listProjectSharesOut struct {
	Users  []shareUserOut  `json:"users"`
	Groups []shareGroupOut `json:"groups"`
}

func (r *registrar) registerSharingTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "share_project", Description: "Share a project with users (by email) and/or groups at read or write permission. Returns any user emails that did not match a tenant member (unresolved) so the caller can correct them. Shares on an org project have no effect — every tenant member already has read+write.", Annotations: mutatingAnno(),
		InputSchema: inputSchema[shareProjectIn](map[string][]any{"permission": {"read", "write"}})},
		func(ctx context.Context, req *sdk.CallToolRequest, in shareProjectIn) (*sdk.CallToolResult, shareProjectOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, shareProjectOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, shareProjectOut{}, errInvalidArg("project_id")
			}
			if in.Permission == "" {
				in.Permission = "read"
			}
			for _, email := range in.Users {
				if !tenant.ValidEmail(email) {
					return nil, shareProjectOut{}, fmt.Errorf("%w: invalid email %q", store.ErrInvalid, email)
				}
			}
			if len(in.Users) == 0 && len(in.Groups) == 0 {
				return nil, shareProjectOut{}, fmt.Errorf("%w: provide users and/or groups", store.ErrInvalid)
			}
			var unresolved []string
			if len(in.Users) > 0 {
				sr, err := r.svc.ShareUsers(ctx, id, pid, in.Users, in.Permission)
				if err != nil {
					return nil, shareProjectOut{}, toolErr(err)
				}
				unresolved = sr.Unresolved
			}
			if len(in.Groups) > 0 {
				if _, err := r.svc.ShareGroups(ctx, id, pid, in.Groups, in.Permission); err != nil {
					return nil, shareProjectOut{}, toolErr(err)
				}
			}
			return nil, shareProjectOut{Unresolved: unresolved}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "unshare_project", Description: "Remove user and/or group shares from a project.", Annotations: mutatingAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in unshareProjectIn) (*sdk.CallToolResult, unshareProjectOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, unshareProjectOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, unshareProjectOut{}, errInvalidArg("project_id")
			}
			if len(in.Users) == 0 && len(in.Groups) == 0 {
				return nil, unshareProjectOut{}, fmt.Errorf("%w: provide users and/or groups", store.ErrInvalid)
			}
			if len(in.Users) > 0 {
				if err := r.svc.UnshareUsers(ctx, id, pid, in.Users); err != nil {
					return nil, unshareProjectOut{}, toolErr(err)
				}
			}
			if len(in.Groups) > 0 {
				if err := r.svc.UnshareGroups(ctx, id, pid, in.Groups); err != nil {
					return nil, unshareProjectOut{}, toolErr(err)
				}
			}
			return nil, unshareProjectOut{Removed: true}, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "list_project_shares", Description: "List the user and group shares of a project.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in projectIDIn) (*sdk.CallToolResult, listProjectSharesOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, listProjectSharesOut{}, err
			}
			pid, err := uuid.Parse(in.ProjectID)
			if err != nil {
				return nil, listProjectSharesOut{}, errInvalidArg("project_id")
			}
			shares, err := r.svc.ListShares(ctx, id, pid)
			if err != nil {
				return nil, listProjectSharesOut{}, toolErr(err)
			}
			out := listProjectSharesOut{
				Users:  make([]shareUserOut, 0, len(shares.Users)),
				Groups: make([]shareGroupOut, 0, len(shares.Groups)),
			}
			for _, u := range shares.Users {
				out.Users = append(out.Users, shareUserOut{Email: u.Email, Permission: u.Permission})
			}
			for _, g := range shares.Groups {
				out.Groups = append(out.Groups, shareGroupOut{Group: g.Group, Permission: g.Permission})
			}
			return nil, out, nil
		})
}
