package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aberti/cnpg-portal/internal/clusteryaml"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/sops"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

// Provision creates a tenant: SOPS-encrypted Secret in the workspace, live
// Secret in the cluster, role + database + grants in Postgres, and a
// managed.roles entry in cluster.yaml. The flow is idempotent end-to-end:
// re-running on partial state completes the missing pieces and reuses the
// existing password.
//
// On success the returned Tenant carries the DSN the new app should use.
// Caller is responsible for handing it to the human (CLI prints it; UI
// renders it once and never persists).
func Provision(ctx context.Context, d Deps, app string) (*Tenant, error) {
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q (must match [a-z][a-z0-9_]{0,62})", app)
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return nil, errors.New("Provision: K8s, PG, and Workspace deps are required")
	}

	secretName := K8sSecretName(app)
	logger := slog.With("verb", "provision", "app", app)

	// 1. Resolve password: existing SOPS file → reuse; missing → generate + encrypt.
	password, err := ensureSecretFile(ctx, d.Workspace, app, secretName)
	if err != nil {
		return nil, fmt.Errorf("ensure secret file: %w", err)
	}

	// 2. Apply Secret in-cluster (idempotent).
	if err := applyClusterSecret(ctx, d, secretName, app, password); err != nil {
		return nil, fmt.Errorf("apply cluster secret: %w", err)
	}
	logger.Info("cluster secret applied", "secret", secretName)

	// 3. Ensure role exists with the current password (CREATE or ALTER).
	if err := ensureRole(ctx, d.PG, app, password); err != nil {
		return nil, fmt.Errorf("ensure role: %w", err)
	}
	logger.Info("role ensured")

	// 4. Ensure database exists, owned by role.
	if err := ensureDatabase(ctx, d.PG, app); err != nil {
		return nil, fmt.Errorf("ensure database: %w", err)
	}
	logger.Info("database ensured")

	// 5. Ensure default grants on public schema.
	if err := ensureGrants(ctx, d.PG, app); err != nil {
		return nil, fmt.Errorf("ensure grants: %w", err)
	}

	// 6. Patch cluster.yaml (idempotent — ErrRoleExists treated as success).
	clusterPath := d.Workspace.ClusterYAMLPath()
	if err := clusteryaml.AppendRole(clusterPath, clusteryaml.Role{
		Name:           app,
		Comment:        app + " application user",
		PasswordSecret: secretName,
	}); err != nil && !errors.Is(err, clusteryaml.ErrRoleExists) {
		return nil, fmt.Errorf("patch cluster.yaml: %w", err)
	}
	logger.Info("cluster.yaml patched", "path", clusterPath)

	return &Tenant{
		Name:        app,
		Role:        app,
		Database:    app,
		SecretName:  secretName,
		DatabaseURL: buildDatabaseURL(d.Workspace.ServiceName, app, password, d.Workspace.Namespace, app),
	}, nil
}

// ensureSecretFile reads the existing SOPS-encrypted Secret if present,
// otherwise generates a password, writes a plaintext Secret YAML, and
// SOPS-encrypts it in place. Returns the password string.
func ensureSecretFile(ctx context.Context, ws *workspace.Workspace, app, secretName string) (string, error) {
	path := ws.SecretPath(app)

	if _, err := os.Stat(path); err == nil {
		decrypted, err := sops.Decrypt(ctx, ws.Root, path)
		if err != nil {
			return "", err
		}
		pw, err := parseSecretPassword(decrypted)
		if err != nil {
			return "", fmt.Errorf("parse decrypted %s: %w", filepath.Base(path), err)
		}
		return pw, nil
	}

	if err := sops.CheckAvailable(); err != nil {
		return "", err
	}
	pw, err := generatePassword()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	body := buildSecretYAML(secretName, ws.Namespace, app, pw)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	if err := sops.EncryptInPlace(ctx, ws.Root, path); err != nil {
		// Don't leave plaintext on disk if encryption failed.
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return "", err
	}
	return pw, nil
}

// parseSecretPassword extracts stringData.password from a Secret YAML.
// We avoid pulling in yaml.v3 here for a single field lookup; tolerant
// line-by-line parsing is enough.
func parseSecretPassword(b []byte) (string, error) {
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		const sk = "password:"
		if !strings.HasPrefix(l, sk) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(l, sk))
		v = strings.Trim(v, `"'`)
		if v == "" {
			continue
		}
		return v, nil
	}
	return "", errors.New("password field not found")
}

// buildSecretYAML emits the SOPS-encrypted-on-disk form of a tenant credential
// Secret. Schema is `kubernetes.io/basic-auth` with both `username` and
// `password` keys — required by CNPG ≥ 1.25 for managed-role passwordSecrets.
// Older Opaque-with-only-`password` secrets are grandfathered by CNPG today
// but will break on a future operator version. New Secrets always use the
// modern schema so a fresh tenant works on the first reconcile.
func buildSecretYAML(name, namespace, username, password string) []byte {
	qu := strings.ReplaceAll(username, "'", "''")
	qp := strings.ReplaceAll(password, "'", "''")
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/basic-auth
stringData:
  username: '%s'
  password: '%s'
`, name, namespace, qu, qp))
}

func applyClusterSecret(ctx context.Context, d Deps, name, username, password string) error {
	ns := d.Workspace.Namespace
	secrets := d.K8s.Clientset.CoreV1().Secrets(ns)
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "cnpg-portal"},
			},
			Type: corev1.SecretTypeBasicAuth,
			StringData: map[string]string{
				"username": username,
				"password": password,
			},
		}, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}
	// Type is immutable; legacy Opaque secrets stay Opaque (CNPG grandfathers
	// them). Only update the credential payload if it actually changed.
	if string(existing.Data["password"]) == password &&
		string(existing.Data["username"]) == username {
		return nil
	}
	existing.StringData = map[string]string{
		"username": username,
		"password": password,
	}
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func ensureRole(ctx context.Context, conn *pg.Conn, role, password string) error {
	qrole, err := pg.QuoteIdent(role)
	if err != nil {
		return err
	}
	pw := pg.LiteralEscape(password)
	stmt := fmt.Sprintf(`DO $$ BEGIN
  CREATE ROLE %s LOGIN PASSWORD '%s';
EXCEPTION WHEN duplicate_object THEN
  ALTER ROLE %s WITH LOGIN PASSWORD '%s';
END $$;
`, qrole, pw, qrole, pw)
	_, err = conn.RunSQL(ctx, "", stmt)
	return err
}

func ensureDatabase(ctx context.Context, conn *pg.Conn, db string) error {
	rows, err := conn.RunQuery(ctx, "", fmt.Sprintf(
		"SELECT 1 FROM pg_database WHERE datname = '%s';", pg.LiteralEscape(db),
	))
	if err != nil {
		return err
	}
	qdb, err := pg.QuoteIdent(db)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		// CREATE DATABASE cannot run inside a transaction block.
		_, err = conn.RunSQL(ctx, "", fmt.Sprintf("CREATE DATABASE %s OWNER %s;", qdb, qdb))
		return err
	}
	// Already exists — make sure ownership is correct (cheap idempotent fix).
	_, err = conn.RunSQL(ctx, "", fmt.Sprintf("ALTER DATABASE %s OWNER TO %s;", qdb, qdb))
	return err
}

func ensureGrants(ctx context.Context, conn *pg.Conn, app string) error {
	q, err := pg.QuoteIdent(app)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO %s;`, q, q)
	if _, err := conn.RunSQL(ctx, "", stmt); err != nil {
		return err
	}
	publicGrants := fmt.Sprintf(`GRANT ALL ON SCHEMA public TO %s;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO %s;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO %s;
`, q, q, q)
	_, err = conn.RunSQL(ctx, app, publicGrants)
	return err
}
