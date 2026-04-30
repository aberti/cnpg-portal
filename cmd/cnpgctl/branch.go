package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newBranchCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "branch <src> <dst>",
		Short: "Clone <src> tenant into a new <dst> tenant via in-cluster pg_dump | pg_restore",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			t, err := tenant.Branch(c.Context(), deps, args[0], args[1])
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "branch complete")
			_, _ = fmt.Fprintln(out, "  source       ", args[0])
			_, _ = fmt.Fprintln(out, "  destination  ", t.Name)
			_, _ = fmt.Fprintln(out, "  cluster.yaml ", deps.Workspace.ClusterYAMLPath())
			_, _ = fmt.Fprintln(out, "  encrypted at ", deps.Workspace.SecretPath(t.Name))
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "DATABASE_URL (handle as a secret — printed once):")
			_, _ = fmt.Fprintln(out, " ", t.DatabaseURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
