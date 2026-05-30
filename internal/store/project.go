package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/project"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/projectgroupshare"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/projectshare"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/tenant"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
)

// loadProject fetches a project within the caller's tenant with shares eager-loaded.
// Returns ErrNotFound if it doesn't exist or belongs to a different tenant.
func (s *Store) loadProject(ctx context.Context, id Identity, projectID uuid.UUID) (*ent.Project, error) {
	p, err := s.client.Project.Query().
		Where(project.IDEQ(projectID), project.HasTenantWith(tenant.IDEQ(id.TenantID))).
		WithOwner().
		WithShares(func(q *ent.ProjectShareQuery) { q.WithUser() }).
		WithGroupShares().
		WithTenant().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.Edges.Tenant.ID != id.TenantID {
		return nil, ErrNotFound // cross-tenant: never reveal existence
	}
	return p, nil
}

// factsOf bridges an ent.Project (with edges loaded) to the pure projectFacts
// struct that effectiveAccess operates on.
func factsOf(p *ent.Project) projectFacts {
	f := projectFacts{
		Visibility:  p.Visibility.String(),
		OwnerID:     p.Edges.Owner.ID,
		UserShares:  map[uuid.UUID]string{},
		GroupShares: map[string]string{},
	}
	for _, sh := range p.Edges.Shares {
		if sh.Edges.User != nil {
			f.UserShares[sh.Edges.User.ID] = sh.Permission.String()
		}
	}
	for _, gs := range p.Edges.GroupShares {
		f.GroupShares[gs.GroupName] = gs.Permission.String()
	}
	return f
}

// requireAccess loads a project and asserts at least `need`, mapping failures to
// ErrNotFound (no access at all) or ErrPermission (read where write needed).
func (s *Store) requireAccess(ctx context.Context, id Identity, projectID uuid.UUID, need Access) (*ent.Project, Access, error) {
	p, err := s.loadProject(ctx, id, projectID)
	if err != nil {
		return nil, NoAccess, err
	}
	got := effectiveAccess(factsOf(p), id)
	if got == NoAccess {
		return nil, NoAccess, ErrNotFound
	}
	if got < need {
		return nil, got, ErrPermission
	}
	return p, got, nil
}

// CreateProject creates a new project owned by the identity's user within their tenant.
func (s *Store) CreateProject(ctx context.Context, id Identity, name, description, visibility string) (*ent.Project, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalid)
	}
	if visibility != "org" && visibility != "private" {
		return nil, fmt.Errorf("%w: visibility must be org|private", ErrInvalid)
	}
	return s.client.Project.Create().
		SetName(name).
		SetDescription(description).
		SetVisibility(project.Visibility(visibility)).
		SetTenantID(id.TenantID).
		SetOwnerID(id.UserID).
		Save(ctx)
}

// GetProject fetches a project by ID, enforcing tenant-scoping and at least ReadAccess.
// Returns ErrNotFound for cross-tenant or no-access projects (existence never revealed).
func (s *Store) GetProject(ctx context.Context, id Identity, projectID uuid.UUID) (*ent.Project, error) {
	p, _, err := s.requireAccess(ctx, id, projectID, ReadAccess)
	return p, err
}

// ListProjects returns every project in the tenant that the caller can at least read.
// Archived projects are omitted unless includeArchived is true.
func (s *Store) ListProjects(ctx context.Context, id Identity, includeArchived bool) ([]*ent.Project, error) {
	all, err := s.client.Project.Query().
		Where(project.HasTenantWith(tenant.IDEQ(id.TenantID))).
		WithOwner().
		WithShares(func(q *ent.ProjectShareQuery) { q.WithUser() }).
		WithGroupShares().
		WithTenant().
		All(ctx)
	if err != nil {
		return nil, err
	}
	var out []*ent.Project
	for _, p := range all {
		if p.Edges.Tenant.ID != id.TenantID {
			continue
		}
		if p.Archived && !includeArchived {
			continue
		}
		if effectiveAccess(factsOf(p), id) > NoAccess {
			out = append(out, p)
		}
	}
	return out, nil
}

// requireOwnerProject loads a project (tenant-scoped) and asserts the caller is its
// owner or a tenant admin. Used by archive/unarchive and sharing operations.
func (s *Store) requireOwnerProject(ctx context.Context, id Identity, projectID uuid.UUID) (*ent.Project, error) {
	p, err := s.loadProject(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	if id.IsAdmin || p.Edges.Owner.ID == id.UserID {
		return p, nil
	}
	// Not owner/admin. Mirror the read paths' existence-hiding rule: a caller with no
	// access at all gets ErrNotFound (never revealing the project exists); only a caller
	// who can see the project but isn't owner/admin gets ErrPermission.
	if effectiveAccess(factsOf(p), id) == NoAccess {
		return nil, ErrNotFound
	}
	return nil, ErrPermission
}

// ArchiveProject hides a project from listings and search (owner/admin only, reversible).
func (s *Store) ArchiveProject(ctx context.Context, id Identity, projectID uuid.UUID) error {
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return err
	}
	return s.client.Project.UpdateOneID(p.ID).SetArchived(true).Exec(ctx)
}

// UnarchiveProject restores a project to listings and search (owner/admin only).
func (s *Store) UnarchiveProject(ctx context.Context, id Identity, projectID uuid.UUID) error {
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return err
	}
	return s.client.Project.UpdateOneID(p.ID).SetArchived(false).Exec(ctx)
}

// ShareResult reports emails that were valid but matched no existing user in the tenant.
type ShareResult struct {
	Unresolved []string
}

// ShareProjectUsers grants per-user access to a project (owner/admin only).
// Each email is resolved to an existing user within the caller's tenant; emails
// that don't match any user are collected in ShareResult.Unresolved (not fatal).
// permission must be "read" or "write".
func (s *Store) ShareProjectUsers(ctx context.Context, id Identity, projectID uuid.UUID, emails []string, permission string) (*ShareResult, error) {
	if permission != "read" && permission != "write" {
		return nil, fmt.Errorf("%w: permission must be read|write", ErrInvalid)
	}
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	res := &ShareResult{}
	for _, email := range emails {
		u, uerr := s.client.User.Query().
			Where(user.EmailEQ(email), user.HasTenantWith(tenant.IDEQ(id.TenantID))).
			Only(ctx)
		if ent.IsNotFound(uerr) {
			res.Unresolved = append(res.Unresolved, email)
			continue
		}
		if uerr != nil {
			return nil, uerr
		}
		// Upsert the share: update permission if it already exists.
		existing, qerr := s.client.ProjectShare.Query().
			Where(projectshare.HasProjectWith(project.IDEQ(p.ID)), projectshare.HasUserWith(user.IDEQ(u.ID))).
			Only(ctx)
		switch {
		case qerr == nil:
			if _, uperr := existing.Update().SetPermission(projectshare.Permission(permission)).Save(ctx); uperr != nil {
				return nil, uperr
			}
		case ent.IsNotFound(qerr):
			if _, cerr := s.client.ProjectShare.Create().
				SetProjectID(p.ID).SetUserID(u.ID).SetCreatedByID(id.UserID).
				SetPermission(projectshare.Permission(permission)).Save(ctx); cerr != nil {
				return nil, cerr
			}
		default:
			return nil, qerr
		}
	}
	return res, nil
}

// ShareProjectGroups grants group-based access to a project (owner/admin only).
// permission must be "read" or "write". Empty group names are skipped.
func (s *Store) ShareProjectGroups(ctx context.Context, id Identity, projectID uuid.UUID, groups []string, permission string) (*ShareResult, error) {
	if permission != "read" && permission != "write" {
		return nil, fmt.Errorf("%w: permission must be read|write", ErrInvalid)
	}
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g == "" {
			continue
		}
		existing, qerr := s.client.ProjectGroupShare.Query().
			Where(projectgroupshare.HasProjectWith(project.IDEQ(p.ID)), projectgroupshare.GroupNameEQ(g)).
			Only(ctx)
		switch {
		case qerr == nil:
			if _, uperr := existing.Update().SetPermission(projectgroupshare.Permission(permission)).Save(ctx); uperr != nil {
				return nil, uperr
			}
		case ent.IsNotFound(qerr):
			if _, cerr := s.client.ProjectGroupShare.Create().
				SetProjectID(p.ID).SetGroupName(g).SetCreatedByID(id.UserID).
				SetPermission(projectgroupshare.Permission(permission)).Save(ctx); cerr != nil {
				return nil, cerr
			}
		default:
			return nil, qerr
		}
	}
	return &ShareResult{}, nil
}

// UnshareProjectUsers removes individual shares by email (owner/admin only).
// Missing shares are silently ignored.
func (s *Store) UnshareProjectUsers(ctx context.Context, id Identity, projectID uuid.UUID, emails []string) error {
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return err
	}
	for _, email := range emails {
		// Tenant-scoped: email is not globally unique, so resolve only within the
		// caller's tenant (mirrors ShareProjectUsers; prevents cross-tenant matches).
		u, uerr := s.client.User.Query().
			Where(user.EmailEQ(email), user.HasTenantWith(tenant.IDEQ(id.TenantID))).
			Only(ctx)
		if ent.IsNotFound(uerr) {
			continue
		}
		if uerr != nil {
			return uerr
		}
		if _, derr := s.client.ProjectShare.Delete().
			Where(projectshare.HasProjectWith(project.IDEQ(p.ID)), projectshare.HasUserWith(user.IDEQ(u.ID))).
			Exec(ctx); derr != nil {
			return derr
		}
	}
	return nil
}

// UnshareProjectGroups removes group shares by name (owner/admin only).
// Missing shares are silently ignored.
func (s *Store) UnshareProjectGroups(ctx context.Context, id Identity, projectID uuid.UUID, groups []string) error {
	p, err := s.requireOwnerProject(ctx, id, projectID)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g == "" {
			continue
		}
		if _, derr := s.client.ProjectGroupShare.Delete().
			Where(projectgroupshare.HasProjectWith(project.IDEQ(p.ID)), projectgroupshare.GroupNameEQ(g)).
			Exec(ctx); derr != nil {
			return derr
		}
	}
	return nil
}
