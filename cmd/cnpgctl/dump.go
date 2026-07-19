package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/pg"
)

func newDumpCmd() *cobra.Command {
	var out string
	var workspaceFlag, kubeconfigFlag string

	cmd := &cobra.Command{
		Use:   "dump <app>",
		Short: "Stream pg_dump -Fc of the tenant database to stdout or a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			app := args[0]
			if !pg.DatabaseNameSafe(app) {
				return fmt.Errorf("invalid database name %q", app)
			}
			deps, err := buildDeps(workspaceFlag, kubeconfigFlag)
			if err != nil {
				return err
			}

			var w *os.File
			switch out {
			case "":
				w = os.Stdout
			default:
				if _, err := os.Stat(out); err == nil {
					return errors.New("refusing to overwrite existing file: " + out)
				}
				w, err = os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return err
				}
				defer func() { _ = w.Close() }()
			}

			if err := deps.PG.PgDump(c.Context(), app, w); err != nil {
				if out != "" {
					_ = os.Remove(out) // don't leave half-written dumps on disk
				}
				return err
			}
			if out != "" {
				_, _ = fmt.Fprintln(c.ErrOrStderr(), "wrote", out)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file (default: stdout). Refuses to overwrite an existing file.")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "path to GitOps workspace")
	cmd.Flags().StringVar(&kubeconfigFlag, "kubeconfig", "", "path to kubeconfig (overrides $KUBECONFIG)")
	return cmd
}
