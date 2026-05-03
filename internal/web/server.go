package web

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/version"
)

// Default HTTP server timing knobs. Exported so flags in cmd/cnpgctl/serve.go
// can hand the same values to cobra without duplicating constants.
const (
	DefaultReadTimeout = 15 * time.Second
	// WriteTimeout must cover long handlers (on-demand backup wait, restore-from-backup).
	DefaultWriteTimeout   = 90 * time.Minute
	DefaultShutdownGrace  = 20 * time.Second
	defaultIdleTimeout    = 60 * time.Second
	defaultMaxHeaderBytes = 1 << 20 // 1 MiB
)

// Config tunes the HTTP server. Zero values fall back to the Default*
// constants in Serve, so callers can set only the knobs they care about.
type Config struct {
	Addr          string
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	ShutdownGrace time.Duration

	// DevLogin replaces the Tailscale-User-Login header read in
	// IdentityMiddleware — used by `cnpgctl serve --dev-login=…` for laptop
	// development without the tailnet. Empty in production.
	DevLogin string

	// TailnetFallbackLogin is optional production use: when set, requests
	// without Tailscale headers but from 100.64.0.0/10 (see identity.go) use
	// this login. For Traefik + tailnet-only DNS without tailscale serve.
	TailnetFallbackLogin string

	// AdminListPath points at a newline-separated file of admin emails
	// (mounted from a ConfigMap in production). Empty path means
	// "no admins configured" — every mutation 403s.
	AdminListPath string

	// BearerToken + BearerIdentity: optional shared secret for HTTP Authorization.
	// Used behind Traefik without Tailscale headers; mount token from a Secret file.
	BearerToken    string
	BearerIdentity string
}

// Serve runs the HTTP server until ctx is cancelled, then drains in-flight
// requests up to cfg.ShutdownGrace before returning. deps may be a zero
// value when the server is only used for /healthz + /metrics smoke tests
// (handlers that need PG/K8s will return a clear 503 in that case).
func Serve(ctx context.Context, cfg Config, deps tenant.Deps, logger *slog.Logger) error {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = DefaultReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = DefaultWriteTimeout
	}
	if cfg.ShutdownGrace == 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}

	admins, err := NewAdminList(cfg.AdminListPath, logger)
	if err != nil {
		return err
	}
	switch {
	case cfg.BearerToken != "" && cfg.BearerIdentity == "":
		return errors.New("serve: --bearer-identity is required when a bearer token is set")
	case cfg.BearerToken == "" && cfg.BearerIdentity != "":
		return errors.New("serve: set --bearer-token or --bearer-token-file when --bearer-identity is set")
	}
	if cfg.AdminListPath == "" && cfg.DevLogin != "" {
		admins.GrantDevFallback(cfg.DevLogin)
	}
	if cfg.AdminListPath == "" && cfg.TailnetFallbackLogin != "" {
		admins.GrantDevFallback(cfg.TailnetFallbackLogin)
	}
	if cfg.BearerToken != "" && cfg.BearerIdentity != "" {
		admins.SetBearerAdmin(cfg.BearerIdentity)
	}
	admins.WatchSIGHUP(ctx)

	if cfg.BearerToken != "" && cfg.BearerIdentity != "" {
		logger.Info("identity: bearer token authentication enabled", "identity", cfg.BearerIdentity)
	}
	if cfg.TailnetFallbackLogin != "" {
		logger.Info("identity: tailnet fallback login enabled (CGNAT 100.64.0.0/10 only)",
			"login", cfg.TailnetFallbackLogin)
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: Router(deps, logger, Auth{
			DevLogin:             cfg.DevLogin,
			TailnetFallbackLogin: cfg.TailnetFallbackLogin,
			BearerToken:          cfg.BearerToken,
			BearerIdentity:       cfg.BearerIdentity,
			Admins:               admins,
		}),
		ReadTimeout:    cfg.ReadTimeout,
		WriteTimeout:   cfg.WriteTimeout,
		IdleTimeout:    defaultIdleTimeout,
		MaxHeaderBytes: defaultMaxHeaderBytes,
		BaseContext:    func(_ net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening",
			"addr", cfg.Addr,
			"version", version.Version,
			"commit", version.Commit,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining", "grace", cfg.ShutdownGrace)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// Router returns the http.Handler tree. Exported so tests can drive it
// directly with httptest. /healthz and /metrics are intentionally outside
// the identity gate so kubelet probes and Prometheus (which don't carry
// tailnet identity) keep working.
func Router(deps tenant.Deps, logger *slog.Logger, auth Auth) http.Handler {
	h := &Handlers{Deps: deps, Logger: logger, Auth: auth}

	r := chi.NewRouter()
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware(logger))
	r.Use(RecovererMiddleware(logger))

	r.Get("/healthz", healthzHandler)
	r.Handle("/metrics", promhttp.Handler())

	r.Group(func(r chi.Router) {
		r.Use(IdentityMiddleware(auth))
		r.Get("/", h.ListTenants)
		r.Get("/tenant/{name}", h.TenantDetail)

		r.Group(func(r chi.Router) {
			r.Use(RequireAdmin(auth.Admins))
			// Data-exfiltration paths are admin-gated. A tenant dump or the
			// active-connections viewer (which renders running query text)
			// are equivalent in capability to read-only access plus a copy
			// of the data; viewer-tier identities only see metadata.
			r.Get("/tenant/{name}/dump", h.DumpTenant)
			r.Get("/tenant/{name}/conns", h.TenantConnections)
			r.Get("/new", h.NewTenantForm)
			r.Post("/new", h.NewTenantSubmit)
			r.Get("/new/import-dump", h.ImportDumpForm)
			r.Post("/new/import-dump", h.ImportDumpSubmit)
			r.Get("/new/import-url", h.ImportURLForm)
			r.Post("/new/import-url", h.ImportURLSubmit)
			r.Post("/tenant/{name}/conns/{pid}/terminate", h.TerminateTenantConnection)
			r.Get("/tenant/{name}/branch", h.BranchTenantForm)
			r.Post("/tenant/{name}/branch", h.BranchTenantSubmit)
			r.Get("/tenant/{name}/sync", h.SyncTenantForm)
			r.Post("/tenant/{name}/sync", h.SyncTenantSubmit)
			r.Get("/tenant/{name}/drop", h.DropTenantForm)
			r.Post("/tenant/{name}/drop", h.DropTenantSubmit)
			r.Get("/tenant/{name}/restore", h.RestoreTenantForm)
			r.Post("/tenant/{name}/restore", h.RestoreTenantSubmit)
			r.Get("/tenant/{name}/rotate", h.RotateTenantForm)
			r.Post("/tenant/{name}/rotate", h.RotateTenantSubmit)
			r.Post("/backup", h.TriggerBackup)
		})
	})

	return r
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
