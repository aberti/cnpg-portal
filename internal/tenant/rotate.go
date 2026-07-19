package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/sops"
)

// RotateOptions controls the credential-rotation flow.
type RotateOptions struct {
	// Confirmed must be true. The CLI sets this from --yes-i-mean-it; the UI
	// sets it after confirm-by-name. Same posture as DropOptions.
	Confirmed bool
}

// Rotate generates a fresh password for the tenant's role, ALTERs the role
// on the cluster, updates the in-cluster Secret, and re-encrypts the SOPS
// Secret file in the workspace. cluster.yaml is unchanged — only the
// Secret's content rotates.
//
// The flow is best-effort idempotent: if a step fails partway through,
// re-running picks a new password and re-applies. The cluster always ends
// up with the role password matching the in-cluster Secret, because CNPG
// reconciles the role from the Secret; the in-cluster ALTER ROLE here is
// belt-and-braces for immediate effect without waiting for reconciliation.
//
// On success the returned Tenant carries the new DSN. The caller must
// commit the updated SOPS file in `<workspace>/secrets/pg/<app>-pg-credentials.sops.yaml`
// to keep GitOps in sync.
func Rotate(ctx context.Context, d Deps, app string, opts RotateOptions) (*Tenant, error) {
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	if !opts.Confirmed {
		return nil, ErrConfirmationRequired
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return nil, errors.New("Rotate: K8s, PG, and Workspace deps are required")
	}

	logger := slog.With("verb", "rotate", "app", app)
	secretName := d.Workspace.SecretName(app)

	// Pre-flight: refuse if there is no dedicated role with this name.
	// Many CNPG databases are owned by `postgres` (legacy migrations,
	// CNPG bootstrap defaults). PGM's list view shows them as tenants
	// because they are databases, but rotate operates on role+secret —
	// without a matching role there is nothing to rotate.
	exists, err := roleExists(ctx, d.PG, app)
	if err != nil {
		return nil, fmt.Errorf("check role exists: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("role %q does not exist on pg-primary; this database has no dedicated user under PGM management (probably owned by `postgres`). Use cnpgctl new <name> to onboard it as a real tenant first", app)
	}

	newPassword, err := generatePassword()
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}

	// 1. ALTER ROLE on cluster — immediate effect.
	if err := alterRolePassword(ctx, d.PG, app, newPassword); err != nil {
		return nil, fmt.Errorf("alter role password: %w", err)
	}
	logger.Info("role password altered")

	// 2. Update in-cluster Secret so CNPG reconciliation keeps the new
	//    password and existing apps reading the Secret pick it up.
	if err := applyClusterSecret(ctx, d, secretName, app, newPassword); err != nil {
		return nil, fmt.Errorf("apply cluster secret: %w", err)
	}
	logger.Info("cluster secret updated", "secret", secretName)

	// 3. Re-encrypt the SOPS Secret file in the workspace.
	if err := writeSOPSSecret(d, app, secretName, newPassword); err != nil {
		return nil, fmt.Errorf("write sops file: %w", err)
	}
	logger.Info("sops file rewritten", "path", d.Workspace.SecretPath(app))

	return &Tenant{
		Name:        app,
		Role:        app,
		Database:    app,
		SecretName:  secretName,
		DatabaseURL: buildDatabaseURL(d.Workspace.ServiceName, app, newPassword, d.Workspace.Namespace, app),
	}, nil
}

// roleExists reports whether a login role with the given name is present
// on the cluster. Used as a Rotate pre-flight so we fail before mutating.
func roleExists(ctx context.Context, conn *pg.Conn, role string) (bool, error) {
	rows, err := conn.RunQuery(ctx, "", fmt.Sprintf(
		"SELECT 1 FROM pg_roles WHERE rolname = '%s';", pg.LiteralEscape(role),
	))
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// alterRolePassword issues ALTER ROLE <role> WITH PASSWORD '<pw>' as the
// superuser. Identifier + password are escaped via internal/pg helpers.
func alterRolePassword(ctx context.Context, conn *pg.Conn, role, password string) error {
	qrole, err := pg.QuoteIdent(role)
	if err != nil {
		return err
	}
	_, err = conn.RunSQL(ctx, "", fmt.Sprintf(
		"ALTER ROLE %s WITH LOGIN PASSWORD '%s';",
		qrole, pg.LiteralEscape(password),
	))
	return err
}

// writeSOPSSecret builds and encrypts a Secret entirely in memory, then
// atomically replaces the existing encrypted file.
func writeSOPSSecret(d Deps, app, secretName, password string) error {
	path := d.Workspace.SecretPath(app)
	body := buildSecretYAML(secretName, d.Workspace.Namespace, app, password)
	encrypted, err := sops.EncryptForWorkspace(d.Workspace.Root, path, body)
	if err != nil {
		return err
	}
	return sops.WriteEncryptedAtomic(path, encrypted)
}
