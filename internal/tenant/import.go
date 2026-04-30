package tenant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os/exec"
	"strings"

	"github.com/aberti/cnpg-portal/internal/pg"
)

// ImportFromDump provisions a tenant then loads an uploaded dump into its database.
// Supports pg_dump -Fc (custom) magic PGDMP… or plain SQL (including gzip not supported — use uncompressed).
func ImportFromDump(ctx context.Context, d Deps, app string, r io.Reader) (*Tenant, error) {
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return nil, errors.New("ImportFromDump: K8s, PG, and Workspace deps are required")
	}
	logger := slog.With("verb", "import_dump", "app", app)

	t, err := Provision(ctx, d, app)
	if err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}
	format, body, err := pg.DetectDumpFormat(r)
	if err != nil {
		return t, fmt.Errorf("read dump: %w", err)
	}
	logger.Info("import dump format detected", "format", format)
	switch format {
	case "custom":
		err = d.PG.RestoreCustomFromReader(ctx, app, body)
	default:
		err = d.PG.RestoreSQLFromReader(ctx, app, body)
	}
	if err != nil {
		return t, fmt.Errorf("load dump: %w", err)
	}
	if err := ensureGrants(ctx, d.PG, app); err != nil {
		return t, fmt.Errorf("ensure grants after import: %w", err)
	}
	logger.Info("import dump complete")
	return t, nil
}

// ImportFromRemoteURL provisions a tenant then runs pg_dump -Fc on the operator machine,
// streaming into pg_restore in-cluster. Requires the pg_dump binary on PATH where cnpgctl runs.
// The DATABASE_URL must use scheme postgres or postgresql and include a reachable host from that process.
func ImportFromRemoteURL(ctx context.Context, d Deps, app, databaseURL string) (*Tenant, error) {
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, fmt.Errorf("empty DATABASE_URL")
	}
	if err := validatePostgresURL(databaseURL); err != nil {
		return nil, err
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return nil, errors.New("ImportFromRemoteURL: K8s, PG, and Workspace deps are required")
	}

	hostForLog := "(unknown)"
	if u, err := url.Parse(databaseURL); err == nil && u.Host != "" {
		hostForLog = u.Hostname()
	}
	if isInClusterHost(hostForLog) {
		return nil, fmt.Errorf("DATABASE_URL host %q is in-cluster DNS only; cnpg-portal runs outside the cluster and cannot resolve it. To clone data between tenants in the same CNPG cluster use `cnpgctl branch <src> <dst>` (logical clone, no network traversal). To import from a real external Postgres, use a hostname reachable from this machine (or open a kubectl port-forward and use 127.0.0.1)", hostForLog)
	}
	slog.Info("import from remote URL", "verb", "import_url", "app", app, "remote_host", hostForLog)

	t, err := Provision(ctx, d, app)
	if err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}

	cmd := exec.CommandContext(ctx, "pg_dump",
		"-Fc", "--no-owner", "--no-privileges",
		"-d", databaseURL,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return t, fmt.Errorf("pg_dump stdout: %w", err)
	}
	var stderrDump strings.Builder
	cmd.Stderr = &stderrDump
	if err := cmd.Start(); err != nil {
		return t, fmt.Errorf("pg_dump start (is pg_dump installed?): %w", err)
	}

	// Always Wait() the pg_dump process so its stderr is captured even when
	// pg_restore fails first (truncated stdin → "input file is too short").
	// Reporting both sides' errors makes the root cause obvious — usually
	// pg_dump couldn't connect or auth.
	restoreErr := d.PG.RestoreCustomFromReader(ctx, app, stdout)
	if cmd.Process != nil && restoreErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	dumpStderr := strings.TrimSpace(stderrDump.String())
	switch {
	case restoreErr != nil && waitErr != nil:
		return t, fmt.Errorf("pg_dump from %s failed: %w (stderr: %s); pg_restore also failed: %w",
			hostForLog, waitErr, dumpStderr, restoreErr)
	case waitErr != nil:
		return t, fmt.Errorf("pg_dump from %s: %w (stderr: %s)", hostForLog, waitErr, dumpStderr)
	case restoreErr != nil:
		return t, fmt.Errorf("pg_restore: %w (pg_dump exited cleanly; stderr: %s)", restoreErr, dumpStderr)
	}

	if err := ensureGrants(ctx, d.PG, app); err != nil {
		return t, fmt.Errorf("ensure grants after import: %w", err)
	}
	slog.Info("import from remote URL complete", "app", app, "remote_host", hostForLog)
	return t, nil
}

// isInClusterHost reports whether the hostname is a Kubernetes-internal DNS
// name (cluster.local domain or bare service.namespace forms). Such names
// only resolve from inside the cluster, so an out-of-cluster pg_dump call
// will fail with a name-resolution error before it reads any data.
func isInClusterHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.HasSuffix(host, ".svc.cluster.local") ||
		strings.HasSuffix(host, ".svc") ||
		strings.HasSuffix(host, ".cluster.local")
}

func validatePostgresURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	default:
		return fmt.Errorf("DATABASE_URL scheme must be postgres or postgresql")
	}
	if u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("DATABASE_URL must include a host")
	}
	return nil
}
