package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newImportURLCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag, fromURL string

	cmd := &cobra.Command{
		Use:   "import-url <app>",
		Short: "Provision a tenant by piping pg_dump -Fc from a remote DATABASE_URL into the cluster",
		Long: "Runs pg_dump -Fc on this machine (must have PostgreSQL client tools on PATH), streams into in-cluster pg_restore.\n" +
			"Does not log the full URL; only the remote hostname appears in audit logs.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if fromURL == "" {
				return fmt.Errorf("--from-url is required")
			}
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			t, err := tenant.ImportFromRemoteURL(c.Context(), deps, args[0], fromURL)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "import from URL complete")
			_, _ = fmt.Fprintln(out, "  app          ", t.Name)
			_, _ = fmt.Fprintln(out, "  workspace    ", deps.Workspace.Root)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "DATABASE_URL (handle as a secret — printed once):")
			_, _ = fmt.Fprintln(out, " ", t.DatabaseURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace (overrides $CNPG_PORTAL_WORKSPACE and CWD walk)")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	cmd.Flags().StringVar(&fromURL, "from-url", "", "source postgres URL (postgres:// or postgresql://)")
	return cmd
}
