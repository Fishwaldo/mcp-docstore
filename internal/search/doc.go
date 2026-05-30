// Package search provides a persistent Bleve full-text index with tenant- and
// access-scoped queries. It knows nothing about ent or the store; callers map
// their domain rows to Doc.
package search

// Doc is the indexable view of a document. The json tags are the Bleve field names.
type Doc struct {
	ID              string   `json:"id"`
	TenantID        string   `json:"tenant_id"`
	ProjectID       string   `json:"project_id"`
	OwnerID         string   `json:"owner_id"`
	Visibility      string   `json:"visibility"` // "org" | "private"
	SharedUserIDs   []string `json:"shared_user_ids"`
	SharedGroups    []string `json:"shared_groups"`
	ProjectArchived bool     `json:"project_archived"`
	Title           string   `json:"title"`
	Overview        string   `json:"overview"`
	Body            string   `json:"body"`
	Tags            []string `json:"tags"`
}

// Query is a search request. Text is plain keywords/phrase (NO Bleve syntax exposed).
// The access fields are filled from the authenticated caller, never from agent input.
type Query struct {
	Text       string
	TenantID   string
	UserID     string
	Groups     []string
	ProjectID  string   // optional filter
	Visibility string   // optional filter: "org" | "private"
	Tags       []string // optional filter (all must match)
	Limit      int      // default 20 if <= 0
}

// Result is one search hit.
type Result struct {
	DocumentID string
	ProjectID  string
	Title      string
	Overview   string
	Score      float64
	Snippet    string // best-matching fragment, if any
}
