// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexReadLoadsProjectFacts(t *testing.T) {
	s := newTestStore(t)
	ctx, id := fixture(t, s)
	p, err := s.CreateProject(ctx, id, "P", "", "private")
	require.NoError(t, err)
	_, err = s.ShareProjectGroups(ctx, id, p.ID, []string{"eng"}, "read")
	require.NoError(t, err)
	d, err := s.CreateDocument(ctx, id, p.ID, NewDocument{Title: "T", Overview: "o", Body: "b", Tags: []string{"x"}})
	require.NoError(t, err)

	// AllDocumentsForIndex returns docs with project edges loaded.
	all, err := s.AllDocumentsForIndex(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, d.ID, all[0].ID)
	require.NotNil(t, all[0].Edges.Project)
	require.NotNil(t, all[0].Edges.Project.Edges.Owner)
	require.Len(t, all[0].Edges.Project.Edges.GroupShares, 1)

	// DocumentForIndex returns one doc with the same eager-loading.
	one, err := s.DocumentForIndex(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, d.ID, one.ID)
	require.NotNil(t, one.Edges.Project.Edges.Owner)

	// DocumentsByProjectForIndex returns that project's docs.
	byProj, err := s.DocumentsByProjectForIndex(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, byProj, 1)
}
