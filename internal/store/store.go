// Package store owns all database access, tenant scoping, the authorization
// access rule, optimistic concurrency, and version snapshots.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/tenant"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	_ "modernc.org/sqlite"
)

type Store struct {
	client    *ent.Client
	retention int
}

// Option configures a Store.
type Option func(*Store)

// WithSnapshotRetention sets how many historical snapshots to keep (min 0).
func WithSnapshotRetention(n int) Option {
	return func(s *Store) {
		if n >= 0 {
			s.retention = n
		}
	}
}

// Identity is the authenticated caller for a request, resolved by the auth layer
// (Phase 3) and threaded into every store call.
type Identity struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	Groups   []string // from the token's groups claim, current at request time
	IsAdmin  bool
}

// dialectFor maps our config driver name to an ent dialect + database/sql driver name.
func dialectFor(driver string) (entDialect string, sqlDriver string, err error) {
	switch driver {
	case "sqlite":
		return dialect.SQLite, "sqlite", nil
	case "mysql":
		return dialect.MySQL, "mysql", nil
	case "postgres":
		return dialect.Postgres, "pgx", nil
	default:
		return "", "", fmt.Errorf("%w: unknown db driver %q", ErrInvalid, driver)
	}
}

// Open opens a Store backed by the given driver and DSN.
// Supported drivers: "sqlite", "mysql", "postgres".
func Open(driver, dsn string, opts ...Option) (*Store, error) {
	entD, sqlD, err := dialectFor(driver)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqlD, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	drv := entsql.OpenDB(entD, db)
	s := &Store{client: ent.NewClient(ent.Driver(drv)), retention: 10}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// Migrate runs the ent schema migration against the database.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.client.Close() }

// EnsureTenant returns the tenant with the given key, creating it if absent.
//
// TODO(phase4): the check-then-create below is not atomic. Concurrent callers for the
// same key can both miss and both Create; the second hits the unique constraint on key.
// Acceptable in Phase 1 (single-threaded bootstrap), but wrap with an on-conflict re-query
// before serving concurrent HTTP requests.
func (s *Store) EnsureTenant(ctx context.Context, key, name string) (*ent.Tenant, error) {
	t, err := s.client.Tenant.Query().Where(tenant.KeyEQ(key)).Only(ctx)
	if err == nil {
		return t, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	return s.client.Tenant.Create().SetKey(key).SetName(name).Save(ctx)
}

// UpsertUser finds-or-creates a user by external_subject, binding to tenantID and
// refreshing the email. It also reconciles the user's role from the isAdmin flag
// (RoleAdmin when true, RoleMember otherwise) on both create and update, so role is
// always driven by config. external_subject is globally unique, so a subject seen
// under a different tenant is rejected with ErrInvalid (single-tenant binding, spec §3).
func (s *Store) UpsertUser(ctx context.Context, tenantID uuid.UUID, subject, email string, isAdmin bool) (*ent.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role := user.RoleMember
	if isAdmin {
		role = user.RoleAdmin
	}
	existing, err := s.client.User.Query().
		Where(user.ExternalSubjectEQ(subject)).
		WithTenant().
		Only(ctx)
	if err == nil {
		if existing.Edges.Tenant.ID != tenantID {
			return nil, fmt.Errorf("%w: subject already bound to another tenant", ErrInvalid)
		}
		if existing.Email != email || existing.Role != role {
			return existing.Update().SetEmail(email).SetRole(role).Save(ctx)
		}
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	return s.client.User.Create().
		SetExternalSubject(subject).
		SetEmail(email).
		SetTenantID(tenantID).
		SetRole(role).
		Save(ctx)
}
