package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newDropCmd() *cobra.Command {
	var confirm bool
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "drop <app>",
		Short: "Drop a tenant: terminate sessions, DROP DATABASE+ROLE, delete Secret, patch cluster.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			app := args[0]
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			if err := tenant.Drop(c.Context(), deps, app, tenant.DropOptions{Confirmed: confirm}); err != nil {
				return err
			}
			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "tenant dropped")
			_, _ = fmt.Fprintln(out, "  app          ", app)
			_, _ = fmt.Fprintln(out, "  cluster.yaml ", deps.Workspace.ClusterYAMLPath())
			_, _ = fmt.Fprintln(out, "  removed      ", deps.Workspace.SecretPath(app))
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "S3 backups under s3://ot-cnpg-backups/"+deps.Workspace.ClusterName+" are NOT purged — clean up via bucket lifecycle policy if needed.")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Next:")
			_, _ = fmt.Fprintln(out, "  git -C", deps.Workspace.Root, "add <cluster.yaml> <secrets-dir>/")
			_, _ = fmt.Fprintln(out, `  git -C`, deps.Workspace.Root, `commit -m "feat(pg): drop`, app, `database"`)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "yes-i-mean-it", false, "required to perform destructive teardown")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
