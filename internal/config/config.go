// Package config wires sdk-go/vault into a typed Config struct. The
// pattern: read every value through vault, validate at startup, expose
// strongly-typed fields downstream. Anything missing is fatal — fail
// fast so misconfigured deploys never reach steady state.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/plinth-dev/sdk-go/vault"
)

// Config holds every value read at startup. All fields are required
// unless marked otherwise.
type Config struct {
	// Service identity.
	ServiceName    string
	ServiceVersion string
	ModuleName     string
	Environment    string // "production" | "staging" | "dev"

	// HTTP server.
	HTTPAddr string

	// Postgres.
	DatabaseURL string

	// Cerbos PDP.
	CerbosAddress     string
	CerbosTLS         bool
	CerbosAllowBypass bool // dev-only escape hatch; rejected in production

	// OpenTelemetry.
	OTelExporterEndpoint string // empty disables export

	// Audit.
	AuditModeMemory bool // when true, use in-memory MemoryProducer (dev default)
}

// Load reads from /run/secrets/<NAME> first (Kubernetes-mounted secrets),
// then from process env. Missing required values return an error listing
// every key that wasn't found.
func Load() (Config, error) {
	r := vault.New(
		vault.FileSource("/run/secrets"),
		vault.EnvSource(""),
	)

	get := func(key string) string {
		v, _ := r.Get(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		ServiceName:          get("SERVICE_NAME"),
		ServiceVersion:       firstNonEmpty(get("SERVICE_VERSION"), "0.0.0-dev"),
		ModuleName:           get("MODULE_NAME"),
		Environment:          firstNonEmpty(get("APP_ENV"), "dev"),
		HTTPAddr:             firstNonEmpty(get("HTTP_ADDR"), ":8080"),
		DatabaseURL:          get("DATABASE_URL"),
		CerbosAddress:        firstNonEmpty(get("CERBOS_ADDRESS"), "localhost:3593"),
		CerbosTLS:            parseBool(get("CERBOS_TLS"), false),
		CerbosAllowBypass:    parseBool(get("CERBOS_ALLOW_BYPASS"), false),
		OTelExporterEndpoint: get("OTEL_EXPORTER_OTLP_ENDPOINT"),
		AuditModeMemory:      parseBool(get("AUDIT_MEMORY"), get("APP_ENV") != "production"),
	}

	missing := []string{}
	for _, req := range []struct {
		key   string
		value string
	}{
		{"SERVICE_NAME", cfg.ServiceName},
		{"MODULE_NAME", cfg.ModuleName},
		{"DATABASE_URL", cfg.DatabaseURL},
	} {
		if req.value == "" {
			missing = append(missing, req.key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseBool(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

// FromEnvOrDie loads config and exits the process on error. Suitable
// for `main()`; library code should call Load and propagate.
func FromEnvOrDie() Config {
	cfg, err := Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}
	return cfg
}
