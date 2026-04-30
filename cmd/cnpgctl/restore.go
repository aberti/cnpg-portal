package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/cnpg"
	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newRestoreCmd() *cobra.Command {
	var workspaceFlag, kubeconfigFlag, backupName, targetTimeStr string

	cmd := &cobra.Command{
		Use:   "restore <app>",
		Short: "Replace a tenant database from a CNPG backup (recovery cluster + pg_dump | pg_restore)",
		Long: `Destructive: drops and recreates the tenant database on the primary using data
recovered from the selected Backup CR. The tenant role and Kubernetes Secret are unchanged.

Requires RBAC to create and delete CNPG Cluster CRs in the Postgres namespace.

Optional --target-time selects PITR within WAL (RFC3339 UTC).`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}
			app := strings.TrimSpace(args[0])
			if strings.TrimSpace(backupName) == "" {
				return fmt.Errorf("--backup is required")
			}
			if !cnpg.BackupObjectNameOK(backupName) {
				return fmt.Errorf("invalid backup name %q", backupName)
			}
			var target *time.Time
			if strings.TrimSpace(targetTimeStr) != "" {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(targetTimeStr))
				if err != nil {
					return fmt.Errorf("parse --target-time: %w", err)
				}
				target = &t
			}
			if err := tenant.RestoreFromBackup(c.Context(), deps, app, backupName, target); err != nil {
				return err
			}
			out := c.OutOrStdout()
			_, _ = fmt.Fprintf(out, "restore complete for tenant %q\n", app)
			return nil
		},
	}

	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	cmd.Flags().StringVar(&backupName, "backup", "", "CNPG Backup resource name (required)")
	cmd.Flags().StringVar(&targetTimeStr, "target-time", "", "optional PITR target (RFC3339)")
	return cmd
}
