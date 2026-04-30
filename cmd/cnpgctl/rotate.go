package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newRotateCmd() *cobra.Command {
	var confirm bool
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "rotate <app>",
		Short: "Rotate a tenant's password: generate new, ALTER ROLE, update Secret + SOPS file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			app := args[0]
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			t, err := tenant.Rotate(c.Context(), deps, app, tenant.RotateOptions{Confirmed: confirm})
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			_, _ = fmt.Fprintln(out, "credentials rotated")
			_, _ = fmt.Fprintln(out, "  app          ", t.Name)
			_, _ = fmt.Fprintln(out, "  secret       ", t.SecretName)
			_, _ = fmt.Fprintln(out, "  database url ", t.DatabaseURL)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Treat the URL as a secret. It is shown once; persist it where the app needs it.")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Next:")
			_, _ = fmt.Fprintln(out, "  git -C", deps.Workspace.Root, "add", deps.Workspace.SecretPath(app))
			_, _ = fmt.Fprintln(out, `  git -C`, deps.Workspace.Root, `commit -m "feat(pg): rotate `+app+` credentials"`)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "yes-i-mean-it", false, "required to perform the rotation")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
