package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/cnpg"
)

func newBackupCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "backup <app>",
		Short: "Trigger an on-demand CNPG Backup CR and follow until completion",
		Long: `Backups in CNPG are cluster-wide (Barman base + WAL), not per-database.
The <app> argument is recorded as the cnpg-portal/triggered-by label on the
Backup CR for audit, but the resulting backup covers all tenants in the
configured cluster (workspace.cluster_name).`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			ns := deps.Workspace.Namespace
			out := c.OutOrStdout()

			b, err := cnpg.Trigger(c.Context(), deps.K8s.Dynamic, ns, deps.Workspace.ClusterName, args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out, "backup triggered:", b.Name)
			_, _ = fmt.Fprintln(out, "  waiting until phase ∈ {completed, failed} ...")

			final, err := cnpg.Wait(c.Context(), deps.K8s.Dynamic, ns, b.Name)
			if err != nil {
				if errors.Is(err, cnpg.ErrBackupFailed) {
					return fmt.Errorf("%w: see kubectl describe backup -n %s %s", err, ns, b.Name)
				}
				return err
			}
			_, _ = fmt.Fprintln(out, "  phase        ", final.Phase)
			if !final.StartedAt.IsZero() {
				_, _ = fmt.Fprintln(out, "  started      ", final.StartedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			if !final.StoppedAt.IsZero() {
				_, _ = fmt.Fprintln(out, "  stopped      ", final.StoppedAt.Format("2006-01-02T15:04:05Z07:00"))
				_, _ = fmt.Fprintln(out, "  duration     ", final.StoppedAt.Sub(final.StartedAt).Round(0))
			}
			if final.DestPath != "" {
				_, _ = fmt.Fprintln(out, "  destination  ", final.DestPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
