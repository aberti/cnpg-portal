package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cnpgctl:", err)
		os.Exit(1)
	}
}

// run owns the cancellable context so deferred cleanup runs before exit.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	var logLevel string
	var logFormat string

	cmd := &cobra.Command{
		Use:           "cnpgctl",
		Short:         "cnpg-portal CLI — self-service DB platform over CloudNativePG",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			logger, err := newLogger(logLevel, logFormat)
			if err != nil {
				return err
			}
			slog.SetDefault(logger)
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")
	cmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "log format: text|json")

	cmd.AddCommand(
		newVersionCmd(),
		newServeCmd(),
		newNewCmd(),
		newListCmd(),
		newStatusCmd(),
		newPsqlCmd(),
		newBranchCmd(),
		newBackupCmd(),
		newDumpCmd(),
		newDropCmd(),
		newRestoreCmd(),
		newImportDumpCmd(),
		newImportURLCmd(),
		newRotateCmd(),
	)

	return cmd
}

func newLogger(level, format string) (*slog.Logger, error) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid log level %q", level)
	}

	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stderr, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(os.Stderr, opts)), nil
	default:
		return nil, fmt.Errorf("invalid log format %q", format)
	}
}
