# Plinth — API module starter

> **Status: not yet released — Phase C in progress.**
> The repo exists; the SDK packages it will import are still being designed in [`sdk-go`](https://github.com/plinth-dev/sdk-go) (Phase B). The "Quick start" and "What v0.1.0 ships with" sections below describe the **target shape**, not what you'd clone today. Track progress at [plinth.run](https://plinth.run) and on the [roadmap](https://github.com/plinth-dev/.github/blob/main/ROADMAP.md).

A clone-ready Go 1.23 module starter that imports `github.com/plinth-dev/sdk-go/*` packages. Authentication enforcement, fail-closed authorization, audit emission, OTel tracing, structured logging, healthcheck, and a strict three-layer architecture — all pre-wired.

## Quick start (target — Phase C)

```bash
git clone https://github.com/plinth-dev/starter-api my-module-api
cd my-module-api
go mod download
go run ./cmd/server
```

For everything-running-locally:

```bash
docker compose up --build      # Postgres, Cerbos, NATS, SigNoz, the module
```

## What v0.1.0 ships with (target)

- **Go 1.23** with `chi` router and `log/slog` structured logging.
- **Three-layer architecture**: `internal/handlers/` → `internal/service/` → `internal/repository/`. Boundaries enforced via Go's `internal/` visibility rules and a CI lint.
- **Database**: `pgx` (v5) + `sqlc` for type-safe queries; `golang-migrate` for reversible migrations.
- **Authorization**: `authz.NewClient(...).CheckAction(...)` — fail-closed by default, dev-bypass requires explicit env + non-prod environment.
- **Audit**: `audit.Publish(...)` to NATS JetStream; never blocks the request path.
- **OTel**: traces and metrics via OTLP/HTTP to the cluster collector.
- **Validation**: `go-playground/validator` on request bodies.
- **Config**: `koanf` (env, file, defaults — composable, no scope creep).
- **Tests**: `testcontainers-go` against real Postgres / NATS / Redis. No mocks of the database.
- **OpenAPI**: generated from struct tags via `swaggo`.

## Planned layout

```
.
├── cmd/
│   └── server/                # main.go — wire dependencies, start chi
├── internal/
│   ├── handlers/              # HTTP handlers; thin
│   ├── service/               # Business logic; testable
│   ├── repository/            # sqlc-generated queries + custom pgx
│   ├── apperrors/             # Sentinel errors used across layers
│   └── ...
├── api/                       # OpenAPI spec output
├── db/
│   ├── migrations/            # golang-migrate SQL files
│   └── queries/               # sqlc input
├── cerbos/                    # Resource policies (one kind per module by default)
└── docker-compose.yml         # Local dev stack
```

## Customisation checklist

After cloning, search the repo for `// TODO: Customize for your module` and walk each one. Then update:

1. `go.mod` — module path: `github.com/<org>/<module>-api`.
2. `internal/config/config.go` — env var prefix.
3. `cerbos/*.yaml` — replace `Item` with your resource kind.

## Related

- [`starter-web`](https://github.com/plinth-dev/starter-web) — the matching Next.js frontend.
- [`sdk-go`](https://github.com/plinth-dev/sdk-go) — the SDK packages this starter imports.
- [`cli`](https://github.com/plinth-dev/cli) — `plinth new` automates the rename + register flow.

## License

MIT — see [LICENSE](./LICENSE).
