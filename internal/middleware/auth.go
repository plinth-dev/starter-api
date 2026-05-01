// Package middleware bundles the cross-cutting HTTP middleware the
// starter wires by default. Auth is intentionally a thin starter shim
// — replace `Auth` below with your project's real auth (OIDC, JWT
// validation, Auth0, Clerk, Stack, etc.) before running in production.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	apperrors "github.com/plinth-dev/sdk-go/errors"
	"go.opentelemetry.io/otel/trace"

	"github.com/plinth-dev/starter-api/internal/service"
)

type ctxKey int

const authCtxKey ctxKey = iota

// Auth is a starter-grade authentication middleware. It reads
// `Authorization: Bearer <token>` and parses the token as
// `<userid>:<role1>,<role2>` — a contract that ONLY makes sense for
// local development. In production, replace this with real JWT or
// session validation.
//
// Anonymous requests get a non-fatal `AuthContext{}` with empty UserID
// and roles; downstream service authz returns Denied for any non-public
// action. This is deliberate: health probes and other public endpoints
// must be reachable without a token.
func Auth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac := service.AuthContext{
				TraceID: traceIDFromContext(r.Context()),
			}

			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
				ac.JWT = token
				if userID, roles, ok := parseStarterToken(token); ok {
					ac.UserID = userID
					ac.Roles = roles
				}
			}

			ctx := context.WithValue(r.Context(), authCtxKey, ac)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthFromContext extracts the AuthContext set by `Auth`. Returns
// apperrors.Unauthenticated when missing — handlers that require a
// caller should call this and SetError on failure.
func AuthFromContext(ctx context.Context) (service.AuthContext, error) {
	ac, ok := ctx.Value(authCtxKey).(service.AuthContext)
	if !ok {
		return service.AuthContext{}, apperrors.Unauthenticated("no auth context")
	}
	if ac.UserID == "" {
		return service.AuthContext{}, apperrors.Unauthenticated("authentication required")
	}
	return ac, nil
}

// MaybeAuthFromContext returns the AuthContext even when anonymous —
// handlers that allow anonymous access (public reads, etc.) use this.
func MaybeAuthFromContext(ctx context.Context) service.AuthContext {
	ac, _ := ctx.Value(authCtxKey).(service.AuthContext)
	return ac
}

// parseStarterToken decodes the starter's dev-only token format:
// `<userid>:<role1>,<role2>`. Returns ok=false on malformed input.
func parseStarterToken(token string) (userID string, roles []string, ok bool) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", nil, false
	}
	userID = parts[0]
	if parts[1] != "" {
		for _, r := range strings.Split(parts[1], ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				roles = append(roles, r)
			}
		}
	}
	return userID, roles, true
}

func traceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

var _ = errors.New // keep import even if Auth grows lighter
