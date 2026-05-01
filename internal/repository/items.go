// Package repository owns SQL access. Hand-written pgx queries —
// straightforward enough for a starter that we don't need sqlc's code
// generation step. Swap for sqlc if your module grows past ~10 queries.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apperrors "github.com/plinth-dev/sdk-go/errors"
	"github.com/plinth-dev/sdk-go/paginate"
)

// Item is the canonical domain type. Match the SQL schema 1:1; no
// transformation between layers.
type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ItemsRepo wraps a pgx connection pool. Keep the API tight — every
// method is a single SQL operation.
type ItemsRepo struct {
	pool *pgxpool.Pool
}

func NewItemsRepo(pool *pgxpool.Pool) *ItemsRepo {
	return &ItemsRepo{pool: pool}
}

// Create inserts a new item. Caller supplies name + status + ownerID;
// id, createdAt, updatedAt are server-generated.
func (r *ItemsRepo) Create(ctx context.Context, name, status, ownerID string) (Item, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	const q = `
		INSERT INTO items (id, name, status, owner_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING id, name, status, owner_id, created_at, updated_at
	`
	var it Item
	err := r.pool.QueryRow(ctx, q, id, name, status, ownerID, now).Scan(
		&it.ID, &it.Name, &it.Status, &it.OwnerID, &it.CreatedAt, &it.UpdatedAt,
	)
	if err != nil {
		return Item{}, apperrors.Wrap(err, apperrors.CodeInternal, "create item")
	}
	return it, nil
}

// Get returns a single item by id, or apperrors.NotFound when absent.
func (r *ItemsRepo) Get(ctx context.Context, id string) (Item, error) {
	const q = `
		SELECT id, name, status, owner_id, created_at, updated_at
		FROM items WHERE id = $1
	`
	var it Item
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&it.ID, &it.Name, &it.Status, &it.OwnerID, &it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperrors.NotFound("Item", id)
	}
	if err != nil {
		return Item{}, apperrors.Wrap(err, apperrors.CodeInternal, "get item")
	}
	return it, nil
}

// Update mutates name / status only. owner_id and timestamps are
// server-managed.
func (r *ItemsRepo) Update(ctx context.Context, id, name, status string) (Item, error) {
	now := time.Now().UTC()
	const q = `
		UPDATE items
		SET name = $2, status = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, name, status, owner_id, created_at, updated_at
	`
	var it Item
	err := r.pool.QueryRow(ctx, q, id, name, status, now).Scan(
		&it.ID, &it.Name, &it.Status, &it.OwnerID, &it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperrors.NotFound("Item", id)
	}
	if err != nil {
		return Item{}, apperrors.Wrap(err, apperrors.CodeInternal, "update item")
	}
	return it, nil
}

// Delete removes an item by id. Returns apperrors.NotFound when no row
// matched — surface this so the handler returns 404 not 204.
func (r *ItemsRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM items WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return apperrors.Wrap(err, apperrors.CodeInternal, "delete item")
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("Item", id)
	}
	return nil
}

// ListParams mirrors what parseTableSearchParams emits on the TS side.
// SortBy is enforced against an allow-list at the handler boundary;
// arbitrary values reach the repo only because the handler dropped
// them — the SQL is built with a switch, never string concatenation.
type ListParams struct {
	Pagination paginate.Pagination
	Search     string
	Status     string
	OwnerID    string // empty means "any owner"
}

// List returns a paginated set of items in offset mode. Cursor mode is
// implementable by switching the WHERE on lastItemCursor — left out of
// the starter to keep the example readable.
func (r *ItemsRepo) List(ctx context.Context, p ListParams) (paginate.Page[Item], error) {
	whereClauses := []string{"1 = 1"}
	args := []any{}

	if p.Search != "" {
		args = append(args, "%"+p.Search+"%")
		whereClauses = append(whereClauses, "name ILIKE $"+itoa(len(args)))
	}
	if p.Status != "" {
		args = append(args, p.Status)
		whereClauses = append(whereClauses, "status = $"+itoa(len(args)))
	}
	if p.OwnerID != "" {
		args = append(args, p.OwnerID)
		whereClauses = append(whereClauses, "owner_id = $"+itoa(len(args)))
	}
	where := joinAnd(whereClauses)

	// Allow-listed sort. The handler should have validated p.Pagination.SortBy,
	// but we re-validate here as defence in depth.
	orderBy := allowedSort(p.Pagination.SortBy, p.Pagination.SortOrder)

	limit := int64(p.Pagination.PageSize)
	offset := int64(p.Pagination.Page-1) * int64(p.Pagination.PageSize)
	args = append(args, limit, offset)

	listQuery := "SELECT id, name, status, owner_id, created_at, updated_at FROM items WHERE " +
		where + " ORDER BY " + orderBy + " LIMIT $" + itoa(len(args)-1) + " OFFSET $" + itoa(len(args))
	countQuery := "SELECT COUNT(*) FROM items WHERE " + where

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return paginate.Page[Item]{}, apperrors.Wrap(err, apperrors.CodeInternal, "list items")
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Status, &it.OwnerID, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return paginate.Page[Item]{}, apperrors.Wrap(err, apperrors.CodeInternal, "scan item")
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return paginate.Page[Item]{}, apperrors.Wrap(err, apperrors.CodeInternal, "iterate items")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args[:len(args)-2]...).Scan(&total); err != nil {
		return paginate.Page[Item]{}, apperrors.Wrap(err, apperrors.CodeInternal, "count items")
	}

	return paginate.NewOffsetPage(items, p.Pagination, total), nil
}

func allowedSort(column string, order paginate.SortOrder) string {
	col := "created_at"
	switch column {
	case "name", "status", "created_at", "updated_at":
		col = column
	}
	dir := "DESC"
	if order == paginate.SortAsc {
		dir = "ASC"
	}
	return col + " " + dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func joinAnd(parts []string) string {
	if len(parts) == 0 {
		return "1 = 1"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " AND " + p
	}
	return out
}
