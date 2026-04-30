package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newStatusCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "status <app>",
		Short: "Per-tenant focused status view (size, connections, role attributes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			t, err := tenant.Status(c.Context(), deps, args[0])
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			connLimit := fmt.Sprintf("%d", t.ConnectionLimit)
			if t.ConnectionLimit < 0 {
				connLimit = "unlimited"
			}
			lastBackup := t.LastBackup
			if lastBackup == "" {
				lastBackup = "-"
			}
			_, _ = fmt.Fprintln(out, "tenant", t.Name)
			_, _ = fmt.Fprintln(out, "  database         ", t.Database)
			_, _ = fmt.Fprintln(out, "  role             ", t.Role)
			_, _ = fmt.Fprintln(out, "  owner            ", t.Owner)
			_, _ = fmt.Fprintln(out, "  size             ", tenant.FormatBytes(t.SizeBytes))
			_, _ = fmt.Fprintln(out, "  connections      ", t.Connections)
			_, _ = fmt.Fprintln(out, "  connection limit ", connLimit)
			_, _ = fmt.Fprintln(out, "  login            ", t.Login)
			_, _ = fmt.Fprintln(out, "  createdb         ", t.CreateDB)
			_, _ = fmt.Fprintln(out, "  inherit          ", t.Inherit)
			_, _ = fmt.Fprintln(out, "  last backup      ", lastBackup)
			return nil
		},
	}

	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace (overrides $CNPG_PORTAL_WORKSPACE and CWD walk)")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
