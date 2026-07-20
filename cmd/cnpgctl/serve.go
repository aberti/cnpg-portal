package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/web"
)

func newServeCmd() *cobra.Command {
	cfg := web.Config{}
	var workspaceFlag, kubeconfigFlag, bearerTokenFile string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the cnpg-portal web UI + HTTP API",
		RunE: func(c *cobra.Command, _ []string) error {
			// Env-var fallbacks for the auth flags so `air` / `make dev` can
			// run a freshly-rebuilt binary without flag plumbing in
			// .air.toml — operator just exports the values once. Flags
			// always win when both are set.
			if cfg.DevLogin == "" {
				cfg.DevLogin = os.Getenv("CNPG_PORTAL_DEV_LOGIN")
			}
			if cfg.AdminListPath == "" {
				cfg.AdminListPath = os.Getenv("CNPG_PORTAL_ADMIN_LIST")
			}
			if bearerTokenFile != "" && cfg.BearerToken != "" {
				return fmt.Errorf("use either --bearer-token or --bearer-token-file, not both")
			}
			if bearerTokenFile != "" {
				b, err := os.ReadFile(bearerTokenFile)
				if err != nil {
					return fmt.Errorf("read bearer token file: %w", err)
				}
				cfg.BearerToken = strings.TrimSpace(string(b))
			}
			registry, err := buildClusterRegistry(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			return web.ServeClusters(c.Context(), cfg, registry, slog.Default())
		},
	}

	cmd.Flags().StringVar(&cfg.Addr, "addr", ":8080", "listen address")
	cmd.Flags().DurationVar(&cfg.ReadTimeout, "read-timeout", web.DefaultReadTimeout, "HTTP read timeout")
	cmd.Flags().DurationVar(&cfg.WriteTimeout, "write-timeout", web.DefaultWriteTimeout, "HTTP write timeout")
	cmd.Flags().DurationVar(&cfg.ShutdownGrace, "shutdown-grace", web.DefaultShutdownGrace, "graceful shutdown timeout")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace (overrides $CNPG_PORTAL_WORKSPACE and CWD walk)")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	cmd.Flags().StringVar(&cfg.DevLogin, "dev-login", "", "bypass tailnet identity headers with this login (laptop dev only — falls back to $CNPG_PORTAL_DEV_LOGIN; leave empty in production)")
	cmd.Flags().StringVar(&cfg.TailnetFallbackLogin, "tailnet-fallback-login", "", "when Tailscale-User-Login is missing, use this login if client IP is in 100.64.0.0/10 (tailnet)")
	cmd.Flags().StringVar(&cfg.AdminListPath, "admin-list-path", "", "path to newline-separated admin email allow-list (falls back to $CNPG_PORTAL_ADMIN_LIST). If empty: --dev-login or --tailnet-fallback-login becomes admin; if all empty, mutations 403 until a list exists")
	cmd.Flags().StringVar(&cfg.BearerToken, "bearer-token", "", "shared secret for Authorization: Bearer (prefer --bearer-token-file in prod)")
	cmd.Flags().StringVar(&bearerTokenFile, "bearer-token-file", "", "read bearer token from file (e.g. Kubernetes Secret volumeMount)")
	cmd.Flags().StringVar(&cfg.BearerIdentity, "bearer-identity", "", "email for audit logs when using bearer auth; bearer-authenticated requests are always admin")
	cmd.Flags().StringSliceVar(&cfg.AllowedOrigins, "allowed-origin", nil, "public origin accepted for POST requests behind a proxy (repeatable, e.g. https://portal.example.com)")

	return cmd
}
