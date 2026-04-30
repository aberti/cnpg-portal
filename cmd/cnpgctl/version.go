package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aberti/cnpg-portal/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print cnpgctl version, commit, and build date",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"cnpgctl %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.BuildDate,
			)
			return err
		},
	}
}
