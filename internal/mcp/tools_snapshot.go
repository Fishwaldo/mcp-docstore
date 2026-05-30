// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type listSnapshotsOut struct {
	Snapshots []snapshotMetaOut `json:"snapshots"`
}
type getSnapshotIn struct {
	DocumentID string `json:"document_id" jsonschema:"the document id"`
	Version    int    `json:"version" jsonschema:"the snapshot version"`
}
type diffVersionsIn struct {
	DocumentID  string `json:"document_id" jsonschema:"the document id"`
	FromVersion int    `json:"from_version" jsonschema:"the older version"`
	ToVersion   int    `json:"to_version" jsonschema:"the newer version"`
}
type diffVersionsOut struct {
	Diff string `json:"diff"`
}

func (r *registrar) registerSnapshotTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "list_snapshots", Description: "List version snapshots for a document (metadata only).", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in documentIDIn) (*sdk.CallToolResult, listSnapshotsOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, listSnapshotsOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, listSnapshotsOut{}, errInvalidArg("document_id")
			}
			snaps, err := r.svc.ListSnapshots(ctx, id, did)
			if err != nil {
				return nil, listSnapshotsOut{}, toolErr(err)
			}
			out := listSnapshotsOut{Snapshots: make([]snapshotMetaOut, 0, len(snaps))}
			for _, s := range snaps {
				out.Snapshots = append(out.Snapshots, toSnapshotMeta(s))
			}
			return nil, out, nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "get_snapshot", Description: "Get a full document snapshot at a specific version.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in getSnapshotIn) (*sdk.CallToolResult, snapshotOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, snapshotOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, snapshotOut{}, errInvalidArg("document_id")
			}
			s, err := r.svc.GetSnapshot(ctx, id, did, in.Version)
			if err != nil {
				return nil, snapshotOut{}, toolErr(err)
			}
			return nil, toSnapshotOut(s), nil
		})

	sdk.AddTool(srv, &sdk.Tool{Name: "diff_versions", Description: "Produce a unified diff of a document between two versions.", Annotations: readOnlyAnno()},
		func(ctx context.Context, req *sdk.CallToolRequest, in diffVersionsIn) (*sdk.CallToolResult, diffVersionsOut, error) {
			id, err := r.ident(req)
			if err != nil {
				return nil, diffVersionsOut{}, err
			}
			did, err := uuid.Parse(in.DocumentID)
			if err != nil {
				return nil, diffVersionsOut{}, errInvalidArg("document_id")
			}
			diff, err := r.svc.DiffVersions(ctx, id, did, in.FromVersion, in.ToVersion)
			if err != nil {
				return nil, diffVersionsOut{}, toolErr(err)
			}
			return nil, diffVersionsOut{Diff: diff}, nil
		})
}
