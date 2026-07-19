package web

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/version"
	"github.com/aberti/cnpg-portal/internal/web/templates"
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

	// AllowedOrigins lists public origins accepted by the CSRF guard in
	// addition to the request's internal Host.
	AllowedOrigins []string
}

// Serve runs the HTTP server until ctx is cancelled, then drains in-flight
// requests up to cfg.ShutdownGrace before returning. deps may be a zero
// value when the server is only used for /healthz + /metrics smoke tests
// (handlers that need PG/K8s will return a clear 503 in that case).
func Serve(ctx context.Context, cfg Config, deps tenant.Deps, logger *slog.Logger) error {
	registry, err := NewClusterRegistry("default", []ClusterTarget{{
		ID:          "default",
		DisplayName: "Default cluster",
		Deps:        deps,
	}})
	if err != nil {
		return err
	}
	return ServeClusters(ctx, cfg, registry, logger)
}

// ServeClusters runs the web UI for every configured CNPG target.
func ServeClusters(ctx context.Context, cfg Config, registry ClusterRegistry, logger *slog.Logger) error {
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
		Handler: RouterClusters(registry, logger, Auth{
			DevLogin:             cfg.DevLogin,
			TailnetFallbackLogin: cfg.TailnetFallbackLogin,
			BearerToken:          cfg.BearerToken,
			BearerIdentity:       cfg.BearerIdentity,
			Admins:               admins,
			AllowedOrigins:       cfg.AllowedOrigins,
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
	baseMiddleware(r, logger, auth.AllowedOrigins)
	registerAssets(r)
	r.Get("/healthz", healthzHandler)
	r.Handle("/metrics", promhttp.Handler())
	r.Group(func(r chi.Router) {
		r.Use(IdentityMiddleware(auth))
		registerRoutes(r, h)
	})
	return r
}

// RouterClusters returns a path-scoped router. Cluster identity is embedded in
// every URL so switching a cluster in one tab cannot retarget another tab's
// destructive form submission.
func RouterClusters(registry ClusterRegistry, logger *slog.Logger, auth Auth) http.Handler {
	r := chi.NewRouter()
	baseMiddleware(r, logger, auth.AllowedOrigins)
	registerAssets(r)
	r.Get("/healthz", healthzHandler)
	r.Handle("/metrics", promhttp.Handler())

	r.Group(func(r chi.Router) {
		r.Use(IdentityMiddleware(auth))
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/clusters/"+registry.Default+"/", http.StatusSeeOther)
		})
		r.Get("/clusters", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/clusters/"+registry.Default+"/", http.StatusSeeOther)
		})
		for _, id := range registry.IDs() {
			target := registry.Targets[id]
			h := &Handlers{
				Deps:        target.Deps,
				Logger:      logger,
				Auth:        auth,
				ClusterID:   target.ID,
				DisplayName: target.DisplayName,
				BasePath:    "/clusters/" + target.ID,
			}
			r.Route(h.BasePath, func(r chi.Router) {
				r.Use(pageContextMiddleware(registry, target, auth))
				registerRoutes(r, h)
			})
		}
	})

	return r
}

func baseMiddleware(r *chi.Mux, logger *slog.Logger, allowedOrigins []string) {
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware(logger))
	r.Use(RecovererMiddleware(logger))
	r.Use(SecurityHeadersMiddleware)
	r.Use(SameOriginMiddleware(allowedOrigins))
}

func registerRoutes(r chi.Router, h *Handlers) {
	r.Get("/", h.ListTenants)
	r.Get("/tenant/{name}", h.TenantDetail)

	r.Group(func(r chi.Router) {
		r.Use(RequireAdmin(h.Auth.Admins))
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
}

func pageContextMiddleware(registry ClusterRegistry, target ClusterTarget, auth Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := IdentityFrom(r.Context())
			page := templates.PageContext{
				BasePath:    "/clusters/" + target.ID,
				ClusterID:   target.ID,
				ClusterName: target.ClusterName(),
				DisplayName: target.DisplayName,
				Namespace:   target.Namespace(),
				Login:       id.Login,
				IsAdmin:     auth.Admins != nil && auth.Admins.Has(id.Login),
			}
			for _, clusterID := range registry.IDs() {
				item := registry.Targets[clusterID]
				page.Clusters = append(page.Clusters, templates.ClusterOption{
					ID:          item.ID,
					DisplayName: item.DisplayName,
					ClusterName: item.ClusterName(),
					Namespace:   item.Namespace(),
					URL:         "/clusters/" + item.ID + "/",
					Active:      item.ID == target.ID,
				})
			}
			ctx := templates.WithPageContext(r.Context(), page)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SecurityHeadersMiddleware keeps credential-bearing pages out of caches and
// constrains all executable content to the locally served application.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// SameOriginMiddleware blocks cross-site browser mutations. PGM does not use
// cookie auth, but Tailscale injects identity headers after accepting a
// cross-origin form, so an Origin check is still required.
func SameOriginMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if canonical, ok := canonicalOrigin(origin); ok {
			allowed[canonical] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
			origin, ok := canonicalOrigin(rawOrigin)
			expectedHTTP, _ := canonicalOrigin("http://" + r.Host)
			expectedHTTPS, _ := canonicalOrigin("https://" + r.Host)
			_, explicitlyAllowed := allowed[origin]
			if ok && (origin == expectedHTTP || origin == expectedHTTPS || explicitlyAllowed) {
				next.ServeHTTP(w, r)
				return
			}

			// Some privacy-focused browsers omit Origin (or serialize it as
			// "null") on a same-origin form submission. Sec-Fetch-Site is a
			// forbidden browser-controlled header, so it is a safe fallback
			// for these two cases. A supplied non-null Origin always takes
			// precedence and cannot be overridden by Fetch Metadata.
			if (rawOrigin == "" || rawOrigin == "null") &&
				r.Header.Get("Sec-Fetch-Site") == "same-origin" {
				next.ServeHTTP(w, r)
				return
			}

			if rawOrigin == "" {
				http.Error(w, "origin required", http.StatusForbidden)
				return
			}
			if !ok || origin != expectedHTTP && origin != expectedHTTPS && !explicitlyAllowed {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		})
	}
}

// canonicalOrigin compares equivalent browser origins without weakening the
// host boundary. Browsers and proxies may serialize the default port or a
// trailing slash differently, although both identify the same origin.
func canonicalOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}

	authority := host
	if port != "" {
		authority = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	return scheme + "://" + authority, true
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
