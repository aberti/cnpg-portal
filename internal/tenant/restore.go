package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aberti/cnpg-portal/internal/cnpg"
	"github.com/aberti/cnpg-portal/internal/pg"
)

// RestoreFromBackup replaces a tenant's database with logical contents recovered
// from a CNPG Backup CR: it provisions a temporary recovery Cluster, streams
// pg_dump | pg_restore into the live primary, then deletes the temp Cluster.
// The tenant role and Secret are unchanged. Destructive to the tenant database.
func RestoreFromBackup(ctx context.Context, d Deps, app, backupName string, targetTime *time.Time) error {
	if !pg.IdentSafe(app) {
		return fmt.Errorf("invalid tenant name %q", app)
	}
	backupName = strings.TrimSpace(backupName)
	if !cnpg.BackupObjectNameOK(backupName) {
		return fmt.Errorf("invalid backup name %q", backupName)
	}
	if d.K8s == nil || d.PG == nil || d.Workspace == nil {
		return errors.New("RestoreFromBackup: K8s, PG, and Workspace deps are required")
	}

	logger := slog.With("verb", "restore", "app", app, "backup", backupName)

	backups, err := cnpg.ListBackups(ctx, d.K8s.Dynamic, d.Workspace.Namespace, d.Workspace.ClusterName)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	var chosen *cnpg.Backup
	for i := range backups {
		if backups[i].Name == backupName {
			chosen = &backups[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("backup %q not found for cluster %s", backupName, d.Workspace.ClusterName)
	}
	if chosen.Phase != cnpg.PhaseCompleted {
		return fmt.Errorf("backup %q is not completed (phase=%s)", backupName, chosen.Phase)
	}
	if chosen.BackupID == "" {
		return fmt.Errorf("backup %q has no status.backupId; cannot restore with Barman Cloud plugin", backupName)
	}
	if chosen.Cluster != "" && chosen.Cluster != d.Workspace.ClusterName {
		return fmt.Errorf("backup %q targets cluster %q, expected %s", backupName, chosen.Cluster, d.Workspace.ClusterName)
	}

	base, err := cnpg.GetCluster(ctx, d.K8s.Dynamic, d.Workspace.Namespace, d.Workspace.ClusterName)
	if err != nil {
		return err
	}

	recName := cnpg.RecoveryClusterName(app)
	if err := cnpg.CreateRecoveryCluster(ctx, d.K8s.Dynamic, d.Workspace.Namespace, recName, chosen.BackupID, base, targetTime); err != nil {
		return err
	}
	deleted := false
	defer func() {
		if deleted {
			return
		}
		ctx2, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := cnpg.DeleteCluster(ctx2, d.K8s.Dynamic, d.Workspace.Namespace, recName); err != nil {
			logger.Error("cleanup recovery cluster failed", "cluster", recName, "err", err)
		}
	}()

	podName, err := cnpg.WaitPrimaryPod(ctx, d.K8s.Clientset, d.Workspace.Namespace, recName, 5*time.Second)
	if err != nil {
		return fmt.Errorf("wait recovery primary pod: %w", err)
	}

	recConn := &pg.Conn{
		K8s:       d.K8s,
		Namespace: d.Workspace.Namespace,
		Pod:       podName,
		Container: d.Workspace.Container,
	}

	// Wait until Postgres on the recovery primary accepts connections to the
	// tenant DB. Each probe uses RunQuery (psql -c, no stdin) with a per-call
	// 15s timeout and an outer 5m cap. This is belt-and-braces after
	// WaitPrimaryPod: the pod's Ready condition normally implies postgres is
	// up, but the operator's instance manager can flag Ready a hair before
	// the database is fully open during recovery bootstrap.
	logger.Info("recovery primary pod ready", "pod", podName)
	waitCtx, cancelWait := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelWait()
	for {
		callCtx, cancelCall := context.WithTimeout(waitCtx, 15*time.Second)
		_, err := recConn.RunQuery(callCtx, app, "SELECT 1")
		cancelCall()
		if err == nil {
			break
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("recovery cluster database %q not reachable: %w", app, waitCtx.Err())
		case <-time.After(3 * time.Second):
		}
	}

	q, err := pg.QuoteIdent(app)
	if err != nil {
		return err
	}
	terminateSQL := fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();",
		pg.LiteralEscape(app),
	)
	if _, err := d.PG.RunSQL(ctx, "", terminateSQL); err != nil {
		logger.Warn("terminate sessions on primary failed (ignored)", "err", err)
	}
	if _, err := d.PG.RunSQL(ctx, "", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", q)); err != nil {
		return fmt.Errorf("drop database on primary: %w", err)
	}
	if err := ensureDatabase(ctx, d.PG, app); err != nil {
		return fmt.Errorf("create empty database on primary: %w", err)
	}

	logger.Info("streaming pg_dump from recovery cluster to primary", "recoveryPod", podName)
	if err := pg.CrossPodDumpRestore(ctx, recConn, d.PG, app, app); err != nil {
		return fmt.Errorf("logical restore: %w", err)
	}
	if err := ensureGrants(ctx, d.PG, app); err != nil {
		return fmt.Errorf("ensure grants after restore: %w", err)
	}

	if err := cnpg.DeleteCluster(ctx, d.K8s.Dynamic, d.Workspace.Namespace, recName); err != nil {
		return fmt.Errorf("delete recovery cluster %s: %w", recName, err)
	}
	deleted = true
	logger.Info("restore complete", "recoveryCluster", recName)
	return nil
}
