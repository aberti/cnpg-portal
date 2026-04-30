package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newImportDumpCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag, file string

	cmd := &cobra.Command{
		Use:   "import-dump <app>",
		Short: "Provision a tenant and load data from a pg_dump -Fc or plain SQL file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			f, err := os.Open(file)
			if err != nil {
				return fmt.Errorf("open dump file: %w", err)
			}
			defer func() { _ = f.Close() }()

			t, err := tenant.ImportFromDump(c.Context(), deps, args[0], f)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "import complete")
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
	cmd.Flags().StringVar(&file, "file", "", "path to pg_dump -Fc custom dump or plain SQL file")
	return cmd
}
