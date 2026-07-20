package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aberti/cnpg-portal/internal/clusteryaml"
	"github.com/aberti/cnpg-portal/internal/pg"
)

// DropOptions controls the destructive teardown.
type DropOptions struct {
	// Confirmed must be true for Drop to proceed. The CLI sets this from
	// --yes-i-mean-it; the UI sets it after the user types the tenant name
	// in a confirmation modal.
	Confirmed bool
}

// ErrConfirmationRequired is returned when a destructive verb is called
// without Confirmed (currently Drop and Rotate). A sentinel rather than a
// panic so callers can surface a targeted error message.
var ErrConfirmationRequired = errors.New("confirmation required (CLI: --yes-i-mean-it; UI: type tenant name)")

// Drop tears down a tenant in the order that avoids dependency conflicts:
// terminate live sessions → DROP DATABASE → DROP ROLE → delete in-cluster
// Secret → patch cluster.yaml (RemoveRole) → delete the SOPS-encrypted
// Secret file. Each step is idempotent; re-running on partial state
// completes the cleanup. S3 backups are *not* purged (manual / lifecycle
// policy on the bucket).
func Drop(ctx context.Context, d Deps, app string, opts DropOptions) error {
	if !pg.IdentSafe(app) {
		return fmt.Errorf("invalid tenant name %q", app)
	}
	if !opts.Confirmed {
		return ErrConfirmationRequired
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return errors.New("Drop: K8s, PG, and Workspace deps are required")
	}

	q, err := pg.QuoteIdent(app)
	if err != nil {
		return err
	}
	logger := slog.With("verb", "drop", "app", app)

	// 1. Terminate live sessions targeting the database. Best-effort: if
	//    there are none, the SELECT returns zero rows and that's fine.
	terminateSQL := fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();",
		pg.LiteralEscape(app),
	)
	if _, err := d.PG.RunSQL(ctx, "", terminateSQL); err != nil {
		logger.Warn("terminate sessions failed (ignored)", "err", err)
	}

	// 2. DROP DATABASE — IF EXISTS makes this idempotent.
	if _, err := d.PG.RunSQL(ctx, "", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", q)); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	logger.Info("database dropped")

	// 3. DROP ROLE — must come after the database (role can't be dropped
	//    while it owns objects).
	if _, err := d.PG.RunSQL(ctx, "", fmt.Sprintf("DROP ROLE IF EXISTS %s;", q)); err != nil {
		return fmt.Errorf("drop role: %w", err)
	}
	logger.Info("role dropped")

	// 4. Delete in-cluster Secret.
	secretName := d.Workspace.SecretName(app)
	delErr := d.K8s.Clientset.CoreV1().Secrets(d.Workspace.Namespace).
		Delete(ctx, secretName, metav1.DeleteOptions{})
	if delErr != nil && !k8serrors.IsNotFound(delErr) {
		return fmt.Errorf("delete secret: %w", delErr)
	}
	logger.Info("cluster secret deleted", "secret", secretName)

	// 5. Patch cluster.yaml.
	if err := clusteryaml.RemoveRole(d.Workspace.ClusterYAMLPath(), app); err != nil {
		return fmt.Errorf("patch cluster.yaml: %w", err)
	}
	logger.Info("cluster.yaml patched", "path", d.Workspace.ClusterYAMLPath())

	// 6. Remove the SOPS-encrypted Secret file from the workspace.
	secretPath := d.Workspace.SecretPath(app)
	if err := os.Remove(secretPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sops file: %w", err)
	}
	logger.Info("sops file removed", "path", secretPath)

	return nil
}
