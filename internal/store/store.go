// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package store owns all database access, tenant scoping, the authorization
// access rule, optimistic concurrency, and version snapshots.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/tenant"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	tenantcfg "github.com/Fishwaldo/mcp-docstore/internal/tenant"
	_ "github.com/go-sql-driver/mysql" // registers "mysql" driver
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	_ "modernc.org/sqlite"             // registers "sqlite" driver
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
// and threaded into every store call.
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
	if driver == "sqlite" {
		if err := assertForeignKeysEnabled(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	drv := entsql.OpenDB(entD, db)
	s := &Store{client: ent.NewClient(ent.Driver(drv)), retention: 10}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// assertForeignKeysEnabled fails fast when the sqlite connection has foreign keys off.
// Cascade deletes (DeleteProject removing documents/snapshots/shares) depend on FK
// enforcement; without it the cascade silently orphans rows. The pragma is per-connection
// in sqlite, so it must be set in the DSN (e.g. _pragma=foreign_keys(1)). mysql/postgres
// enforce FKs natively and are not checked here.
func assertForeignKeysEnabled(db *sql.DB) error {
	var on int
	if err := db.QueryRow("PRAGMA foreign_keys;").Scan(&on); err != nil {
		return fmt.Errorf("check sqlite foreign_keys pragma: %w", err)
	}
	if on == 0 {
		return fmt.Errorf("%w: sqlite foreign keys are disabled; add _pragma=foreign_keys(1) to the DSN so cascade deletes don't orphan rows", ErrInvalid)
	}
	return nil
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

// EntClient returns the underlying ent client, so the composition root (cmd/server) can build
// other ent-backed services — the embedded OAuth authorization server's entstore, in
// particular — against this same connection pool rather than opening a second one. Package
// store itself never needs this; it exists solely for that top-layer wiring.
func (s *Store) EntClient() *ent.Client { return s.client }

// EnsureTenant returns the tenant with the given key, creating it if absent.
//
// TODO: the check-then-create below is not atomic. Concurrent callers for the same key
// can both miss and both Create; the second then hits the unique constraint on key. This
// is fine for single-threaded startup seeding (the only current caller), but wrap it with
// an on-conflict re-query before calling it from concurrent request paths.
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
// under a different tenant is rejected with ErrInvalid: a user belongs to exactly one tenant.
//
// A concurrent first login for the same subject can have two requests both miss the initial
// query and both Create; the second Create then violates the unique external_subject index.
// That loser re-queries the now-existing row and reconciles it like any returning user, so
// the request still succeeds with the same single-tenant binding.
func (s *Store) UpsertUser(ctx context.Context, tenantID uuid.UUID, subject, email string, isAdmin bool) (*ent.User, error) {
	if subject == "" {
		return nil, fmt.Errorf("%w: empty subject", ErrInvalid)
	}
	email = tenantcfg.Normalize(email)
	role := user.RoleMember
	if isAdmin {
		role = user.RoleAdmin
	}

	existing, err := s.findUserBySubject(ctx, subject)
	if err == nil {
		return s.reconcileUser(ctx, existing, tenantID, email, role)
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	created, err := s.client.User.Create().
		SetExternalSubject(subject).
		SetEmail(email).
		SetTenantID(tenantID).
		SetRole(role).
		Save(ctx)
	if err == nil {
		return created, nil
	}
	if !ent.IsConstraintError(err) {
		return nil, err
	}
	// Lost a first-login race: the row now exists. Re-query and reconcile it.
	existing, qerr := s.findUserBySubject(ctx, subject)
	if qerr != nil {
		return nil, err // surface the original constraint error if the row is unexpectedly gone
	}
	return s.reconcileUser(ctx, existing, tenantID, email, role)
}

// findUserBySubject loads the user with the given external_subject, eager-loading its
// tenant edge so reconcileUser can enforce the single-tenant binding.
func (s *Store) findUserBySubject(ctx context.Context, subject string) (*ent.User, error) {
	return s.client.User.Query().
		Where(user.ExternalSubjectEQ(subject)).
		WithTenant().
		Only(ctx)
}

// reconcileUser enforces that an existing user stays bound to its original tenant and
// refreshes its email/role from config. It rejects a subject already bound to a different
// tenant with ErrInvalid (external_subject is globally unique → single-tenant binding).
func (s *Store) reconcileUser(ctx context.Context, existing *ent.User, tenantID uuid.UUID, email string, role user.Role) (*ent.User, error) {
	if existing.Edges.Tenant.ID != tenantID {
		return nil, fmt.Errorf("%w: subject already bound to another tenant", ErrInvalid)
	}
	if existing.Email != email || existing.Role != role {
		return existing.Update().SetEmail(email).SetRole(role).Save(ctx)
	}
	return existing, nil
}
