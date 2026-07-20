package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

func newPsqlCmd() *cobra.Command {
	var workspaceFlag string

	cmd := &cobra.Command{
		Use:   "psql <app> [-- psql args ...]",
		Short: "Open an interactive psql shell as postgres in the tenant database",
		Long: `Shells out to ` + "`kubectl exec -it`" + ` to attach the calling terminal
directly to psql inside the CNPG primary pod. Connects as the postgres
superuser to the tenant's database; use \c <db> <role> to switch users.

Implementing TTY+SIGWINCH+raw-mode purely in Go via client-go SPDY is
finicky and would duplicate kubectl's well-tested handling, so we shell
out instead. Requires kubectl on PATH (mise pin in .mise.toml provides it).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			app := args[0]
			if !pg.DatabaseNameSafe(app) {
				return fmt.Errorf("invalid database name %q", app)
			}
			extra := args[1:]

			ws, err := workspace.FindCluster(workspaceFlag, clusterFlag)
			if err != nil {
				return err
			}

			if _, err := exec.LookPath("kubectl"); err != nil {
				return errors.New("kubectl not found on PATH (run: mise install)")
			}

			argv := []string{
				"exec", "-it",
				"-n", ws.Namespace,
				ws.Pod, "-c", ws.Container,
				"--",
				"psql", "-U", "postgres", "-d", app,
			}
			argv = append(argv, extra...)
			kc := exec.Command("kubectl", argv...)
			kc.Stdin = os.Stdin
			kc.Stdout = os.Stdout
			kc.Stderr = os.Stderr
			return kc.Run()
		},
	}
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	return cmd
}
