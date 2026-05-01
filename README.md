# Plinth — API module starter

A clone-ready Go module that pre-wires every `github.com/plinth-dev/sdk-go/*` package into a working HTTP service. One sample resource (`items`) shipped end-to-end so the integration of every SDK module is visible from `cmd/server/main.go`.

Pre-wired:
- **Authentication** middleware (starter-grade — replace before production).
- **Authorization** via [Cerbos](https://cerbos.dev) PDP (fail-closed — `CheckAction` returns `Allowed: false, Reason: Unreachable` on any error).
- **Audit** events via `sdk-go/audit` (non-blocking; in-memory producer for dev, swap for NATS / Kafka in production).
- **OpenTelemetry** traces via OTLP/HTTP.
- **RFC 7807 problem+json** error responses via `sdk-go/errors` middleware.
- **Health probes** at `/livez` and `/healthz` / `/readyz`.
- **Pagination** via `sdk-go/paginate` with sort-column allow-listing.
- **Config** via `sdk-go/vault` (Kubernetes secret-mounts → env, layered).
- **Three-layer architecture** — `internal/handlers/` → `internal/service/` → `internal/repository/`. Cross-layer boundaries are enforced by Go's `internal/` visibility rules.

See [plinth.run](https://plinth.run) for the SDK design rationale.

## Quick start

Requirements: Go 1.25+, Docker.

```bash
# Clone and rename for your module.
git clone https://github.com/plinth-dev/starter-api my-module-api
cd my-module-api

# Bring up Postgres + Cerbos.
docker compose up -d postgres cerbos

# Run the API directly (fast iteration).
make run
```

By default the API listens on `:8080`. Try it:

```bash
# Health probe (no auth).
curl -s http://localhost:8080/healthz | jq

# Create an item. The starter's dev-only token format is "<userid>:<role1>,<role2>".
curl -s -X POST http://localhost:8080/items \
     -H "Authorization: Bearer alice:editor" \
     -H "Content-Type: application/json" \
     -d '{"name": "thing", "status": "active"}' | jq

# List with pagination + sorting.
curl -s "http://localhost:8080/items?page=1&pageSize=10&sortBy=created_at&sortOrder=desc" \
     -H "Authorization: Bearer alice:viewer" | jq

# Validation failure → RFC 7807 problem+json.
curl -s -X POST http://localhost:8080/items \
     -H "Authorization: Bearer alice:editor" \
     -H "Content-Type: application/json" \
     -d '{"name": "", "status": "what"}' | jq
```

For a fully containerised stack (API in a container too):

```bash
docker compose --profile full up --build
```

## Layout

```
.
├── cmd/server/                 # main.go — wires every SDK module
├── internal/
│   ├── config/                 # sdk-go/vault → typed Config
│   ├── handlers/               # HTTP handlers; thin
│   ├── service/                # Business logic; authz + audit
│   ├── repository/             # pgx queries
│   └── middleware/             # Auth shim, etc.
├── db/migrations/              # SQL applied by docker-compose's postgres init
├── cerbos/
│   ├── config.yaml             # PDP config
│   └── policies/item.yaml      # Resource policy for `Item`
├── docker-compose.yml          # postgres + cerbos (and the API behind --profile full)
├── Dockerfile                  # Distroless multi-stage build
└── Makefile
```

## How the layers fit

```
                     +-----------------+
       HTTP request  |    Chi router   |
                     +-----------------+
                              |
                  +-----------+-----------+
                  | OTel HTTP middleware  |   sdk-go/otel: starts a span
                  +-----------+-----------+
                              |
                  +-----------+-----------+
                  | Auth middleware       |   internal/middleware: parses bearer,
                  +-----------+-----------+   sets AuthContext on the request
                              |
                  +-----------+-----------+
                  | Errors middleware     |   sdk-go/errors: catches errors set via
                  +-----------+-----------+   apperrors.SetError, renders RFC 7807
                              |
                  +-----------+-----------+
                  |    Handlers           |   internal/handlers: parse + validate +
                  +-----------+-----------+   delegate to service
                              |
                  +-----------+-----------+
                  |    Service            |   internal/service: authz check (Cerbos),
                  +-----------+-----------+   audit emission, business logic
                              |
                  +-----------+-----------+
                  |    Repository         |   internal/repository: pgx SQL
                  +-----------+-----------+
                              |
                          [Postgres]
```

Authz lives in the **service** layer, never in handlers — handlers shouldn't know what "comment on a closed item" means semantically.

## Auth: the starter shim

`internal/middleware/auth.go` parses `Authorization: Bearer <userid>:<role1>,<role2>`. **This is for local development only**, so you can curl with `-H "Authorization: Bearer alice:editor"` and see the authz layer work without standing up a real IdP.

Replace with your project's actual auth before production. The replacement contract:

- Take the `Authorization` header (or session cookie) off the request.
- Validate it. Reject invalid tokens with `apperrors.Unauthenticated(...)`.
- Build a `service.AuthContext{ UserID, Roles, JWT, TraceID }`.
- Stick it on `r.Context()` so `middleware.AuthFromContext(ctx)` can retrieve it.

Drop-in candidates: [Auth0](https://auth0.com), [Clerk](https://clerk.com), [Stack](https://stack-auth.com), [Ory Kratos](https://www.ory.sh/kratos/), or a homegrown OIDC client. The contract is `AuthContext`, not the implementation.

## Cerbos policies

`cerbos/policies/item.yaml` is the starter policy for the `Item` resource: any authenticated role can read / list / create; only the owner (or anyone with the `admin` role) can update / delete. Tweak this for your domain — Cerbos hot-reloads on file change.

The dev `cerbos/config.yaml` enables the disk driver pointing at `./policies` and disables Cerbos's own audit log (we have our own audit pipeline). For production, swap to git-driven storage.

## Customisation checklist

After cloning:

1. `go.mod` — change the module path: `github.com/<org>/<module>-api`.
2. `internal/config/config.go` — adjust required env keys for your service.
3. `cerbos/policies/` — replace `Item` with your resource kind(s).
4. `db/migrations/` — replace the `items` table with your schema.
5. `internal/repository/items.go`, `internal/service/items.go`, `internal/handlers/items.go` — rename / extend for your resource(s).
6. `cmd/server/main.go` — register your additional handlers.
7. **Replace `internal/middleware/auth.go` with real auth** before going to production.

## Production hardening

The starter is *clone-ready*, not *production-ready out of the box*. Before deploying:

- Replace the auth middleware (above).
- Swap the audit `MemoryProducer` for a NATS / Kafka producer; otherwise audit events vanish on restart.
- Set `CERBOS_TLS=true` and supply a real CA bundle if your Cerbos PDP has TLS.
- Set `OTEL_EXPORTER_OTLP_ENDPOINT` to your collector; the default endpoint of `http://otel-collector.observability:4318` is for an in-cluster default.
- Run `db/migrations` via your migration tool of choice (golang-migrate, atlas, dbmate). The starter's `docker-compose` only runs them on first volume creation.
- Ensure secrets land at `/run/secrets/<KEY>` (Kubernetes default) — `sdk-go/vault` reads file-mounted secrets first, then env.

## Related

- [`starter-web`](https://github.com/plinth-dev/starter-web) — the matching Next.js frontend.
- [`sdk-go`](https://github.com/plinth-dev/sdk-go) — the SDK packages this starter imports.
- [`platform`](https://github.com/plinth-dev/platform) — the Kubernetes Helm chart that runs the surrounding observability + auth stack.

## License

MIT — see [LICENSE](./LICENSE).
