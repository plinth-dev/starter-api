// Package handlers exposes the items service over HTTP. Handlers are
// thin: parse, call the service, write JSON. All authz / audit / business
// rules live a layer down.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	apperrors "github.com/plinth-dev/sdk-go/errors"
	"github.com/plinth-dev/sdk-go/paginate"

	"github.com/plinth-dev/starter-api/internal/middleware"
	"github.com/plinth-dev/starter-api/internal/repository"
	"github.com/plinth-dev/starter-api/internal/service"
)

// Items wires the service to chi routes.
type Items struct {
	svc *service.Items
}

func NewItems(svc *service.Items) *Items { return &Items{svc: svc} }

// Mount registers /items routes on the supplied router. Handlers
// surface apperrors via SetError so the global errors.HTTPMiddleware
// renders RFC 7807 problem+json.
func (h *Items) Mount(r chi.Router) {
	r.Route("/items", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Get("/{id}", h.get)
		r.Put("/{id}", h.update)
		r.Delete("/{id}", h.delete)
	})
}

// ── request DTOs ─────────────────────────────────────────────────

type createReq struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (c createReq) validate() error {
	fields := map[string]string{}
	if c.Name == "" {
		fields["name"] = "required"
	}
	if len(c.Name) > 120 {
		fields["name"] = "too long (max 120)"
	}
	if c.Status != "active" && c.Status != "archived" {
		fields["status"] = `must be "active" or "archived"`
	}
	if len(fields) > 0 {
		return apperrors.Validation("validation failed", fields)
	}
	return nil
}

type updateReq struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (u updateReq) validate() error {
	return createReq{Name: u.Name, Status: u.Status}.validate()
}

// ── handlers ─────────────────────────────────────────────────────

func (h *Items) create(w http.ResponseWriter, r *http.Request) {
	var body createReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperrors.SetError(r, apperrors.Validation("invalid JSON body", nil))
		return
	}
	if err := body.validate(); err != nil {
		apperrors.SetError(r, err)
		return
	}

	ac, err := middleware.AuthFromContext(r.Context())
	if err != nil {
		apperrors.SetError(r, err)
		return
	}

	it, err := h.svc.Create(r.Context(), ac, body.Name, body.Status)
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

func (h *Items) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ac, err := middleware.AuthFromContext(r.Context())
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	it, err := h.svc.Get(r.Context(), ac, id)
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (h *Items) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body updateReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperrors.SetError(r, apperrors.Validation("invalid JSON body", nil))
		return
	}
	if err := body.validate(); err != nil {
		apperrors.SetError(r, err)
		return
	}
	ac, err := middleware.AuthFromContext(r.Context())
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	it, err := h.svc.Update(r.Context(), ac, id, body.Name, body.Status)
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (h *Items) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ac, err := middleware.AuthFromContext(r.Context())
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), ac, id); err != nil {
		apperrors.SetError(r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Items) list(w http.ResponseWriter, r *http.Request) {
	ac, err := middleware.AuthFromContext(r.Context())
	if err != nil {
		apperrors.SetError(r, err)
		return
	}

	q := r.URL.Query()
	pg, err := paginate.FromQuery(q, []string{"name", "status", "created_at", "updated_at"})
	if err != nil {
		apperrors.SetError(r, apperrors.Validation("invalid pagination", map[string]string{"_root": err.Error()}))
		return
	}

	params := repository.ListParams{
		Pagination: pg,
		Search:     q.Get("q"),
		Status:     q.Get("status"),
	}

	page, err := h.svc.List(r.Context(), ac, params)
	if err != nil {
		apperrors.SetError(r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// ── helpers ──────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}
