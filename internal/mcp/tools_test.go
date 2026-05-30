// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func TestGetSectionTool(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "# A\nalpha\n\n# B\nbeta"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_section",
		Arguments: map[string]any{"document_id": d.ID.String(), "heading": "A"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, structID(t, res, "content"), "alpha")
}

func TestAppendDocumentTool(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "# A\nalpha"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "append_document",
		Arguments: map[string]any{"document_id": d.ID.String(), "text": "more"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestSearchDocumentsTool(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	_, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "findme"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search_documents",
		Arguments: map[string]any{"query": "findme"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	m, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	results, ok := m["results"].([]any)
	require.True(t, ok, "results should be an array")
	require.GreaterOrEqual(t, len(results), 1)
}

func TestSharePrjInvalidEmailIsError(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "share_project",
		Arguments: map[string]any{"project_id": pid.String(), "users": []string{"not-an-email"}},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestListProjectSharesTool(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "share_project",
		Arguments: map[string]any{"project_id": pid.String(), "groups": []string{"eng"}, "permission": "read"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "list_project_shares",
		Arguments: map[string]any{"project_id": pid.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func searchResultCount(t *testing.T, res *sdk.CallToolResult) int {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "structured content should be a JSON object")
	results, ok := m["results"].([]any)
	require.True(t, ok, "results should be an array")
	return len(results)
}

func TestArchiveUnarchiveProjectTools(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	_, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "needle"})
	require.NoError(t, err)

	// sanity: findable before archive
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search_documents",
		Arguments: map[string]any{"query": "needle"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.GreaterOrEqual(t, searchResultCount(t, res), 1)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "archive_project",
		Arguments: map[string]any{"project_id": pid.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search_documents",
		Arguments: map[string]any{"query": "needle"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, 0, searchResultCount(t, res))

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "unarchive_project",
		Arguments: map[string]any{"project_id": pid.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search_documents",
		Arguments: map[string]any{"query": "needle"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.GreaterOrEqual(t, searchResultCount(t, res), 1)
}

func TestListSnapshotsIncludesCreatedBy(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "v1"})
	require.NoError(t, err)
	newBody := "v2"
	_, err = svc.EditReplace(ctx, id, d.ID, d.Version, nil, &newBody, nil, "edit")
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "list_snapshots",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	m, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	snaps, ok := m["snapshots"].([]any)
	require.True(t, ok, "snapshots should be an array")
	require.GreaterOrEqual(t, len(snaps), 1)
	first, ok := snaps[0].(map[string]any)
	require.True(t, ok)
	createdBy, ok := first["created_by"].(string)
	require.True(t, ok, "created_by should be a string")
	require.NotEmpty(t, createdBy)
}

func acceptClient() *sdk.ClientOptions {
	return &sdk.ClientOptions{
		ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			return &sdk.ElicitResult{Action: "accept"}, nil
		},
	}
}

func declineClient() *sdk.ClientOptions {
	return &sdk.ClientOptions{
		ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			return &sdk.ElicitResult{Action: "decline"}, nil
		},
	}
}

func TestDeleteDocumentElicitAccept(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServerWithClient(t, svc, id, acceptClient())
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "body"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "delete_document",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_document",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestDeleteDocumentElicitDecline(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServerWithClient(t, svc, id, declineClient())
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "body"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "delete_document",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_document",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestDeleteDocumentNoElicitRequiresConfirm(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "body"})
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "delete_document",
		Arguments: map[string]any{"document_id": d.ID.String()},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "delete_document",
		Arguments: map[string]any{"document_id": d.ID.String(), "confirm": true},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestRestoreSnapshotElicitAccept(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServerWithClient(t, svc, id, acceptClient())
	ctx := context.Background()

	d, err := svc.CreateDocument(ctx, id, pid, store.NewDocument{Title: "T", Body: "v1"})
	require.NoError(t, err)

	newBody := "v2"
	edited, err := svc.EditReplace(ctx, id, d.ID, d.Version, nil, &newBody, nil, "edit")
	require.NoError(t, err)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "restore_snapshot",
		Arguments: map[string]any{
			"document_id":  d.ID.String(),
			"version":      d.Version,
			"base_version": edited.Version,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestToolAnnotations(t *testing.T) {
	svc, _, id, _ := newSvc(t)
	cs := startServer(t, svc, id)
	res, err := cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	require.NoError(t, err)
	byName := map[string]*sdk.Tool{}
	for _, tl := range res.Tools {
		byName[tl.Name] = tl
	}
	// read-only
	require.NotNil(t, byName["search_documents"].Annotations)
	require.True(t, byName["search_documents"].Annotations.ReadOnlyHint)
	require.True(t, byName["list_projects"].Annotations.ReadOnlyHint)
	// destructive
	require.NotNil(t, byName["delete_document"].Annotations)
	require.False(t, byName["delete_document"].Annotations.ReadOnlyHint)
	require.NotNil(t, byName["delete_document"].Annotations.DestructiveHint)
	require.True(t, *byName["delete_document"].Annotations.DestructiveHint)
	// mutating, non-destructive
	require.False(t, byName["create_document"].Annotations.ReadOnlyHint)
	require.NotNil(t, byName["create_document"].Annotations.DestructiveHint)
	require.False(t, *byName["create_document"].Annotations.DestructiveHint)
	// every tool has annotations + closed-world
	for name, tl := range byName {
		if name == "test" {
			continue
		}
		require.NotNilf(t, tl.Annotations, "tool %q missing annotations", name)
		require.NotNilf(t, tl.Annotations.OpenWorldHint, "tool %q missing OpenWorldHint", name)
		require.Falsef(t, *tl.Annotations.OpenWorldHint, "tool %q should be closed-world", name)
	}
}

func startServer(t *testing.T, svc *Service, id store.Identity) *sdk.ClientSession {
	t.Helper()
	return startServerWithClient(t, svc, id, nil)
}

func startServerWithClient(t *testing.T, svc *Service, id store.Identity, copts *sdk.ClientOptions) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := NewMCPServer(svc, func(*sdk.CallToolRequest) (store.Identity, bool) { return id, true }, nil, nil)
	ct, st := sdk.NewInMemoryTransports()
	_, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, copts)
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })
	return cs
}

// structID pulls a string field out of a tool result's structured content.
func structID(t *testing.T, res *sdk.CallToolResult, field string) string {
	t.Helper()
	m, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "structured content should be a JSON object")
	v, ok := m[field].(string)
	require.True(t, ok, "field %q should be a string", field)
	return v
}

func TestCreateAndGetProjectTool(t *testing.T) {
	svc, _, id, _ := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "create_project",
		Arguments: map[string]any{"name": "Proj", "description": "d", "visibility": "private"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	pid := structID(t, res, "id")
	require.NotEmpty(t, pid)

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_project",
		Arguments: map[string]any{"project_id": pid},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, pid, structID(t, res, "id"))
	require.Equal(t, "Proj", structID(t, res, "name"))
}

func TestListProjectsTool(t *testing.T) {
	svc, _, id, _ := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError)

	m, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	projects, ok := m["projects"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, projects)
	first, ok := projects[0].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, first["access"])
}

func TestEditDocumentSectionTool(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "create_document",
		Arguments: map[string]any{
			"project_id": pid.String(),
			"title":      "T",
			"body":       "# A\nold\n\n# B\nkeep",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	docID := structID(t, res, "id")

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "edit_document",
		Arguments: map[string]any{
			"document_id":  docID,
			"base_version": 1,
			"mode":         "section",
			"heading":      "A",
			"content":      "new",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestEditDocumentInvalidComboIsError(t *testing.T) {
	svc, _, id, pid := newSvc(t)
	cs := startServer(t, svc, id)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "create_document",
		Arguments: map[string]any{
			"project_id": pid.String(),
			"title":      "T",
			"body":       "# A\nbody",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	docID := structID(t, res, "id")

	// section mode with no heading -> invalid
	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name: "edit_document",
		Arguments: map[string]any{
			"document_id":  docID,
			"base_version": 1,
			"mode":         "section",
			"content":      "x",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}
