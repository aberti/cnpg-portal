package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/k8s"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

func newNewCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "new <app>",
		Short: "Provision a new tenant: role, database, Secret, cluster.yaml patch",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			app := args[0]

			ws, err := workspace.FindCluster(workspaceFlag, clusterFlag)
			if err != nil {
				return err
			}
			kube, err := k8s.NewClient(kubeconfigFlag)
			if err != nil {
				return err
			}
			conn := &pg.Conn{
				K8s:       kube,
				Namespace: ws.Namespace,
				Pod:       ws.Pod,
				Container: ws.Container,
			}

			t, err := tenant.Provision(c.Context(), tenant.Deps{
				K8s:       kube,
				PG:        conn,
				Workspace: ws,
			}, app)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "tenant provisioned")
			_, _ = fmt.Fprintln(out, "  app          ", t.Name)
			_, _ = fmt.Fprintln(out, "  role         ", t.Role)
			_, _ = fmt.Fprintln(out, "  database     ", t.Database)
			_, _ = fmt.Fprintln(out, "  secret       ", t.SecretName)
			_, _ = fmt.Fprintln(out, "  workspace    ", ws.Root)
			_, _ = fmt.Fprintln(out, "  cluster.yaml ", ws.ClusterYAMLPath())
			_, _ = fmt.Fprintln(out, "  encrypted at ", ws.SecretPath(app))
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "DATABASE_URL (handle as a secret — printed once):")
			_, _ = fmt.Fprintln(out, " ", t.DatabaseURL)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Next:")
			_, _ = fmt.Fprintln(out, "  git -C", ws.Root, "add <cluster.yaml> <secrets-dir>/"+app+"-pg-credentials.sops.yaml")
			_, _ = fmt.Fprintln(out, `  git -C`, ws.Root, `commit -m "feat(pg): provision`, app, `database"`)
			return nil
		},
	}

	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace (overrides $CNPG_PORTAL_WORKSPACE and CWD walk)")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
