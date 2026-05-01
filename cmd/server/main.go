// Package main is the starter-api entry point. Wire-up only — no
// business logic. Read it top to bottom to understand which Plinth SDK
// modules are integrated and where.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/plinth-dev/sdk-go/audit"
	"github.com/plinth-dev/sdk-go/authz"
	apperrors "github.com/plinth-dev/sdk-go/errors"
	"github.com/plinth-dev/sdk-go/health"
	plinthotel "github.com/plinth-dev/sdk-go/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/plinth-dev/starter-api/internal/config"
	"github.com/plinth-dev/starter-api/internal/handlers"
	"github.com/plinth-dev/starter-api/internal/middleware"
	"github.com/plinth-dev/starter-api/internal/repository"
	"github.com/plinth-dev/starter-api/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.FromEnvOrDie()

	// ── OpenTelemetry ───────────────────────────────────────────
	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	otelShutdown, err := plinthotel.Init(rootCtx, plinthotel.Options{
		ServiceName:      cfg.ServiceName,
		ServiceVersion:   cfg.ServiceVersion,
		ModuleName:       cfg.ModuleName,
		Environment:      cfg.Environment,
		ExporterEndpoint: cfg.OTelExporterEndpoint,
	})
	if err != nil {
		logger.Error("otel init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = otelShutdown(shutdownCtx)
	}()

	// ── Postgres ────────────────────────────────────────────────
	pool, err := pgxpool.New(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres pool init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(rootCtx); err != nil {
		logger.Error("postgres ping failed", slog.Any("error", err))
		os.Exit(1)
	}

	// ── Cerbos PDP ──────────────────────────────────────────────
	// authz reads CERBOS_ALLOW_BYPASS=1 from the env directly (not from
	// Options); EnvName drives the production-bypass safety check.
	authzClient, err := authz.New(rootCtx, authz.Options{
		Address: cfg.CerbosAddress,
		TLS:     cfg.CerbosTLS,
		EnvName: cfg.Environment,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("cerbos init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() { _ = authzClient.Close() }()

	// ── Audit ───────────────────────────────────────────────────
	auditProducer := audit.NewMemoryProducer()
	auditPublisher := audit.New(audit.Options{
		Producer:    auditProducer,
		ServiceName: cfg.ServiceName,
		Logger:      logger,
		TraceIDFunc: traceIDOf,
	})
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = auditPublisher.Close(shutdownCtx)
	}()

	// ── Domain wiring ───────────────────────────────────────────
	itemsRepo := repository.NewItemsRepo(pool)
	itemsSvc := service.NewItems(itemsRepo, authzClient, auditPublisher)
	itemsH := handlers.NewItems(itemsSvc)

	// ── Health probes ───────────────────────────────────────────
	healthReg := health.New(health.WithLogger(logger))
	healthReg.Register(health.PgPing("postgres", pgxAdapter{pool}))
	healthReg.Register(health.CerbosCheck("cerbos", authzClient))

	// ── HTTP router ─────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Mount("/livez", healthReg.LivenessHandler())
	r.Mount("/healthz", healthReg.HTTPHandler())
	r.Mount("/readyz", healthReg.HTTPHandler())

	// API routes — wrapped with otel + auth + errors middleware.
	r.Group(func(api chi.Router) {
		api.Use(func(next http.Handler) http.Handler {
			return plinthotel.HTTPMiddleware(next, plinthotel.WithOperationName("starter-api"))
		})
		api.Use(middleware.Auth())
		api.Use(func(next http.Handler) http.Handler {
			return apperrors.HTTPMiddleware(
				next,
				apperrors.WithLogger(logger),
				apperrors.WithTypeURIPrefix("https://plinth.run/errors/"),
				apperrors.WithTraceIDFunc(traceIDOf),
			)
		})

		itemsH.Mount(api)
	})

	// ── Serve ───────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starter-api listening",
			slog.String("addr", cfg.HTTPAddr),
			slog.String("service", cfg.ServiceName),
			slog.String("module", cfg.ModuleName),
			slog.String("env", cfg.Environment),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", slog.Any("error", err))
			cancel()
		}
	}()

	<-rootCtx.Done()
	logger.Info("shutdown initiated")

	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(shutdownCtx)
}

// pgxAdapter implements health.PingableDB on top of pgxpool.Pool. The
// stdlib database/sql.DB has PingContext; pgx exposes Ping with a
// different signature, so this trivial adapter bridges them.
type pgxAdapter struct{ pool *pgxpool.Pool }

func (a pgxAdapter) PingContext(ctx context.Context) error { return a.pool.Ping(ctx) }

// traceIDOf bridges OTel into sdk-go modules that accept a TraceIDFunc.
// Returning an empty string is intentional when no span is active —
// audit / errors callers fall back to a generated one.
func traceIDOf(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}
