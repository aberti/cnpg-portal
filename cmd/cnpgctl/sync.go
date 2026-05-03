package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newSyncCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string
	var confirm bool

	cmd := &cobra.Command{
		Use:   "sync <src> <dst>",
		Short: "Overwrite <dst> tenant database with <src> via in-cluster pg_dump | pg_restore (DESTRUCTIVE)",
		Long: `Sync replaces the contents of an existing destination tenant with a clone
of an existing source tenant. The destination's role and credential
Secret are preserved; only its database is dropped (WITH FORCE — open
sessions are terminated) and reloaded from pg_dump | pg_restore. Apps
using the destination DSN keep working post-sync.`,
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			src, dst := args[0], args[1]
			if err := tenant.Sync(c.Context(), deps, src, dst, tenant.SyncOptions{Confirmed: confirm}); err != nil {
				return err
			}
			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "sync complete")
			_, _ = fmt.Fprintln(out, "  source       ", src)
			_, _ = fmt.Fprintln(out, "  destination  ", dst)
			_, _ = fmt.Fprintln(out, "  cluster.yaml ", deps.Workspace.ClusterYAMLPath())
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	cmd.Flags().BoolVar(&confirm, "yes-i-mean-it", false, "required: confirms destructive overwrite of <dst>")
	return cmd
}
