// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package search

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func openTemp(t *testing.T) *Index {
	t.Helper()
	idx, err := Open(t.TempDir() + "/idx.bleve")
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestPutDeleteAndCount(t *testing.T) {
	idx := openTemp(t)
	require.NoError(t, idx.Put(Doc{ID: "d1", TenantID: "t1", ProjectID: "p1", Title: "Hello", Body: "world"}))
	require.NoError(t, idx.Put(Doc{ID: "d2", TenantID: "t1", ProjectID: "p1", Title: "Bye", Body: "moon"}))
	n, err := idx.count()
	require.NoError(t, err)
	require.Equal(t, uint64(2), n)

	require.NoError(t, idx.Delete("d1"))
	n, err = idx.count()
	require.NoError(t, err)
	require.Equal(t, uint64(1), n)
}

func TestIsEmpty(t *testing.T) {
	idx := openTemp(t)
	empty, err := idx.IsEmpty()
	require.NoError(t, err)
	require.True(t, empty)

	require.NoError(t, idx.Put(Doc{ID: "d1", TenantID: "t1", ProjectID: "p1", Title: "Hello", Body: "world"}))
	empty, err = idx.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)
}

func TestOpenIsPersistent(t *testing.T) {
	dir := t.TempDir() + "/idx.bleve"
	idx, err := Open(dir)
	require.NoError(t, err)
	require.NoError(t, idx.Put(Doc{ID: "d1", TenantID: "t1", ProjectID: "p1", Title: "persisted"}))
	require.NoError(t, idx.Close())

	// Re-open the same path: data survives (not rebuilt).
	idx2, err := Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx2.Close() })
	n, err := idx2.count()
	require.NoError(t, err)
	require.Equal(t, uint64(1), n)
}

// seed indexes a spread of docs across tenants/visibility/shares/archived.
func seed(t *testing.T, idx *Index) {
	t.Helper()
	docs := []Doc{
		{ID: "own", TenantID: "t1", ProjectID: "p1", OwnerID: "u1", Visibility: "private", Title: "alpha", Body: "shared secret"},
		{ID: "org", TenantID: "t1", ProjectID: "p2", OwnerID: "u9", Visibility: "org", Title: "alpha org", Body: "company wide"},
		{ID: "ushare", TenantID: "t1", ProjectID: "p3", OwnerID: "u9", Visibility: "private", SharedUserIDs: []string{"u1"}, Title: "alpha ushare", Body: "for u1"},
		{ID: "gshare", TenantID: "t1", ProjectID: "p4", OwnerID: "u9", Visibility: "private", SharedGroups: []string{"eng"}, Title: "alpha gshare", Body: "for eng"},
		{ID: "noaccess", TenantID: "t1", ProjectID: "p5", OwnerID: "u9", Visibility: "private", Title: "alpha hidden", Body: "secret"},
		{ID: "archived", TenantID: "t1", ProjectID: "p6", OwnerID: "u1", Visibility: "private", ProjectArchived: true, Title: "alpha archived", Body: "old"},
		{ID: "othertenant", TenantID: "t2", ProjectID: "p7", OwnerID: "u1", Visibility: "org", Title: "alpha foreign", Body: "elsewhere"},
	}
	for _, d := range docs {
		require.NoError(t, idx.Put(d))
	}
}

func ids(res []Result) map[string]bool {
	m := map[string]bool{}
	for _, r := range res {
		m[r.DocumentID] = true
	}
	return m
}

func TestSearchRespectsAccessAndTenant(t *testing.T) {
	idx := openTemp(t)
	seed(t, idx)

	res, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: "u1", Groups: []string{"eng"}})
	require.NoError(t, err)
	got := ids(res)
	require.True(t, got["own"])          // owner
	require.True(t, got["org"])          // org-wide
	require.True(t, got["ushare"])       // individual share
	require.True(t, got["gshare"])       // group share
	require.False(t, got["noaccess"])    // private, no grant
	require.False(t, got["archived"])    // archived excluded even though owned
	require.False(t, got["othertenant"]) // other tenant
}

func TestSearchEmptyUserFailsClosed(t *testing.T) {
	idx := openTemp(t)
	// A private doc with an empty owner_id must NOT leak to a caller with empty UserID.
	require.NoError(t, idx.Put(Doc{ID: "ownerless", TenantID: "t1", ProjectID: "p1", OwnerID: "", Visibility: "private", Title: "alpha orphan"}))
	require.NoError(t, idx.Put(Doc{ID: "orgdoc", TenantID: "t1", ProjectID: "p2", OwnerID: "u9", Visibility: "org", Title: "alpha org"}))

	res, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: ""})
	require.NoError(t, err)
	got := ids(res)
	require.False(t, got["ownerless"]) // empty owner_id must not match empty UserID
	require.True(t, got["orgdoc"])     // org-wide still visible to any tenant member
}

func TestSearchProjectFilter(t *testing.T) {
	idx := openTemp(t)
	seed(t, idx)
	res, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: "u1", ProjectID: "p1"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "own", res[0].DocumentID)
}

func TestSearchNoTextMatchesAllAccessible(t *testing.T) {
	idx := openTemp(t)
	seed(t, idx)
	// Empty text → match-all within the access/tenant/archived filters.
	res, err := idx.Search(Query{TenantID: "t1", UserID: "u1", Groups: []string{"eng"}})
	require.NoError(t, err)
	got := ids(res)
	require.True(t, got["own"] && got["org"] && got["ushare"] && got["gshare"])
	require.False(t, got["noaccess"] || got["archived"] || got["othertenant"])
}

func TestSearchClampsLimit(t *testing.T) {
	idx := openTemp(t)

	// Index more than maxLimit (100) documents, all owned by the same identity in the
	// same tenant (so the caller can read every one) and all sharing the term "alpha".
	const total = 150
	for i := 0; i < total; i++ {
		require.NoError(t, idx.Put(Doc{
			ID: fmt.Sprintf("d%03d", i), TenantID: "t1", ProjectID: "p1", OwnerID: "u1",
			Visibility: "private", Title: "doc", Body: "alpha match",
		}))
	}

	// A pathological limit must be clamped to maxLimit (100), even though 150 hits match.
	res, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: "u1", Limit: 1_000_000_000})
	require.NoError(t, err)
	require.Equal(t, 100, len(res), "limit must be clamped to maxLimit")

	// Limit: 0 falls through to the default (20): returns results, never more than the default.
	res0, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: "u1", Limit: 0})
	require.NoError(t, err)
	require.NotEmpty(t, res0)
	require.LessOrEqual(t, len(res0), 20, "zero limit must use the default cap")
}

// The access disjunction matches a document on identity fields (owner_id,
// shared_user_ids) using the caller's own UUID. Those fields must never be
// highlighted, or the snippet echoes the identity value instead of the text.
func TestSearchSnippetExcludesIdentityFields(t *testing.T) {
	idx := openTemp(t)
	const ownerUUID = "6faa5061-14f5-4115-a92e-5b8cb73ecfc1"
	require.NoError(t, idx.Put(Doc{
		ID: "d1", TenantID: "t1", ProjectID: "p1", OwnerID: ownerUUID,
		Visibility: "private", Title: "notes", Body: "alpha beta gamma",
	}))
	// hit.Fragments is a map; a single call can pick the text field by chance even
	// when identity fields are wrongly highlighted, so assert across many calls.
	for i := 0; i < 30; i++ {
		res, err := idx.Search(Query{Text: "alpha", TenantID: "t1", UserID: ownerUUID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		require.NotContains(t, res[0].Snippet, ownerUUID, "snippet must not echo the owner UUID")
	}
}

func TestPutBatchIndexesMultiple(t *testing.T) {
	idx := openTemp(t)
	docs := []Doc{
		{ID: "b1", TenantID: "t1", ProjectID: "p1", OwnerID: "u1", Visibility: "private", Title: "batch one", Body: "needle alpha"},
		{ID: "b2", TenantID: "t1", ProjectID: "p1", OwnerID: "u1", Visibility: "private", Title: "batch two", Body: "needle beta"},
		{ID: "b3", TenantID: "t1", ProjectID: "p1", OwnerID: "u1", Visibility: "private", Title: "batch three", Body: "needle gamma"},
	}
	require.NoError(t, idx.PutBatch(docs))

	n, err := idx.count()
	require.NoError(t, err)
	require.Equal(t, uint64(3), n)

	res, err := idx.Search(Query{Text: "needle", TenantID: "t1", UserID: "u1"})
	require.NoError(t, err)
	require.Len(t, res, 3)
}

func TestPutBatchEmptyIsNoError(t *testing.T) {
	idx := openTemp(t)
	require.NoError(t, idx.PutBatch(nil))
	n, err := idx.count()
	require.NoError(t, err)
	require.Equal(t, uint64(0), n)
}
