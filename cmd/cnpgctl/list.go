package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newListCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tenants with size, connections, and last backup",
		RunE: func(c *cobra.Command, _ []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			tenants, err := tenant.List(c.Context(), deps)
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "DATABASE\tOWNER\tSIZE\tCONNS\tLAST BACKUP (cluster)")
			for i := range tenants {
				t := &tenants[i]
				lastBackup := t.LastBackup
				if lastBackup == "" {
					lastBackup = "-"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
					t.Name, t.Owner, tenant.FormatBytes(t.SizeBytes), t.Connections, lastBackup,
				)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace (overrides $CNPG_PORTAL_WORKSPACE and CWD walk)")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
