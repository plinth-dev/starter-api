// Package service is the business-logic layer. Authz checks happen
// here, not in handlers — service is the only layer that knows what
// "comment on a closed item" means semantically.
package service

import (
	"context"

	"github.com/plinth-dev/sdk-go/audit"
	"github.com/plinth-dev/sdk-go/authz"
	apperrors "github.com/plinth-dev/sdk-go/errors"

	"github.com/plinth-dev/starter-api/internal/repository"
)

// AuthContext carries the resolved principal for the current request.
// Built once per request by middleware; passed explicitly through the
// service surface so each method's policy is auditable from the
// signature alone.
type AuthContext struct {
	UserID  string
	Roles   []string
	JWT     string // raw token, forwarded to Cerbos for $jwtClaims access
	TraceID string
}

// Items is the items domain service.
type Items struct {
	repo  *repository.ItemsRepo
	authz *authz.Client
	audit *audit.Publisher
}

func NewItems(repo *repository.ItemsRepo, authzClient *authz.Client, auditPublisher *audit.Publisher) *Items {
	return &Items{
		repo:  repo,
		authz: authzClient,
		audit: auditPublisher,
	}
}

// Create requires the principal to have the "create" action on the
// Item kind. Audit is emitted on success; never blocks the request.
func (s *Items) Create(ctx context.Context, ac AuthContext, name, status string) (repository.Item, error) {
	d := s.authz.CheckAction(ctx, principalOf(ac), resourceOf("", nil), "create")
	if !d.Allowed {
		return repository.Item{}, apperrors.PermissionDenied("create item")
	}

	it, err := s.repo.Create(ctx, name, status, ac.UserID)
	if err != nil {
		return repository.Item{}, err
	}

	s.emitAudit(ctx, ac, it.ID, "item.created", audit.OutcomeSuccess, nil, map[string]any{
		"name":   it.Name,
		"status": it.Status,
	})
	return it, nil
}

// Get is read-protected — Cerbos may scope visibility to ownership or
// role.
func (s *Items) Get(ctx context.Context, ac AuthContext, id string) (repository.Item, error) {
	it, err := s.repo.Get(ctx, id)
	if err != nil {
		return repository.Item{}, err
	}
	d := s.authz.CheckAction(ctx, principalOf(ac), resourceOf(it.ID, &it), "read")
	if !d.Allowed {
		// Treat unauthorized reads as 404 to avoid leaking existence.
		return repository.Item{}, apperrors.NotFound("Item", id)
	}
	return it, nil
}

// Update mutates name + status if the principal is authorized.
func (s *Items) Update(ctx context.Context, ac AuthContext, id, name, status string) (repository.Item, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return repository.Item{}, err
	}
	d := s.authz.CheckAction(ctx, principalOf(ac), resourceOf(existing.ID, &existing), "update")
	if !d.Allowed {
		return repository.Item{}, apperrors.PermissionDenied("update item")
	}

	updated, err := s.repo.Update(ctx, id, name, status)
	if err != nil {
		return repository.Item{}, err
	}
	s.emitAudit(ctx, ac, updated.ID, "item.updated", audit.OutcomeSuccess,
		map[string]any{"name": existing.Name, "status": existing.Status},
		map[string]any{"name": updated.Name, "status": updated.Status},
	)
	return updated, nil
}

// Delete removes an item. Generates an audit even when the underlying
// row is already gone, so repeat-delete attempts are visible.
func (s *Items) Delete(ctx context.Context, ac AuthContext, id string) error {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	d := s.authz.CheckAction(ctx, principalOf(ac), resourceOf(existing.ID, &existing), "delete")
	if !d.Allowed {
		return apperrors.PermissionDenied("delete item")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.emitAudit(ctx, ac, id, "item.deleted", audit.OutcomeSuccess, nil, nil)
	return nil
}

// List returns a paginated set; authz happens at row-level inside
// Cerbos's `attr.owner_id == principal.id` filter — at the service
// boundary we just enforce "list" on the kind itself.
func (s *Items) List(ctx context.Context, ac AuthContext, params repository.ListParams) (any, error) {
	d := s.authz.CheckAction(ctx, principalOf(ac), resourceOf("", nil), "list")
	if !d.Allowed {
		return nil, apperrors.PermissionDenied("list items")
	}
	return s.repo.List(ctx, params)
}

// ── helpers ──────────────────────────────────────────────────────

func principalOf(ac AuthContext) authz.Principal {
	return authz.Principal{
		ID:    ac.UserID,
		Roles: ac.Roles,
		AuxData: &authz.AuxData{
			JWT: ac.JWT,
		},
	}
}

// resourceOf builds the Cerbos resource. When `it` is nil (creates,
// lists), only the kind is set; row-level attributes don't exist yet.
func resourceOf(id string, it *repository.Item) authz.Resource {
	r := authz.Resource{Kind: "Item", ID: id}
	if it != nil {
		r.Attributes = map[string]any{
			"owner_id": it.OwnerID,
			"status":   it.Status,
		}
	}
	return r
}

func (s *Items) emitAudit(
	ctx context.Context,
	ac AuthContext,
	resourceID string,
	action string,
	outcome audit.Outcome,
	before, after map[string]any,
) {
	ev := audit.Event{
		Action:  action,
		Outcome: outcome,
		Actor: audit.Actor{
			ID:    ac.UserID,
			Type:  "user",
			Roles: ac.Roles,
		},
		Resource: audit.Resource{
			Kind: "Item",
			ID:   resourceID,
		},
		Before:  before,
		After:   after,
		TraceID: ac.TraceID,
	}
	// audit.Publisher is non-blocking and never returns an error;
	// drops are logged inside the publisher's drain goroutine.
	s.audit.Publish(ctx, ev)
}
