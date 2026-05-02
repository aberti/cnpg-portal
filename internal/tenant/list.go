package tenant

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aberti/cnpg-portal/internal/cnpg"
)

// listSQL queries the cluster's full database inventory minus templates and
// the maintenance "postgres" superuser DB. We join pg_stat_database for live
// connection counts and pg_database_size() for on-disk footprint.
const listSQL = `SELECT
  d.datname,
  pg_catalog.pg_get_userbyid(d.datdba) AS owner,
  pg_database_size(d.datname) AS size_bytes,
  COALESCE(s.numbackends, 0) AS conns
FROM pg_database d
LEFT JOIN pg_stat_database s ON s.datname = d.datname
WHERE NOT d.datistemplate AND d.datname <> 'postgres'
ORDER BY d.datname`

// ClusterName is the CNPG Cluster the tools target — used for Cluster CR
// operations (backup, restore, status). It changes on every major-version
// upgrade (e.g. pg-primary-17 → pg-primary-18).
//
// ServiceName is the *stable* alias used in app connection strings. It maps
// to `<ServiceName>-{rw,ro,r}` Services that select on the current cluster's
// labels (see database/onetask-pg/services-aliases.yaml). Apps and tooling
// that emit DSNs use ServiceName so that a cluster rename never invalidates
// a saved DATABASE_URL. See ADR-0005.
const (
	ClusterName = "pg-primary-17"
	ServiceName = "pg-primary"
)

// List returns the tenant inventory: every non-template, non-postgres
// database in the cluster, with size, connection count, and the last
// cluster-wide CNPG backup time (CNPG's backup is whole-cluster, not
// per-DB, so the same timestamp applies to every row).
func List(ctx context.Context, d Deps) ([]Tenant, error) {
	if d.PG == nil {
		return nil, errors.New("List: PG dep is required")
	}
	rows, err := d.PG.RunQuery(ctx, "", listSQL)
	if err != nil {
		return nil, err
	}

	lastBackup := lastClusterBackupTimestamp(ctx, d)

	out := make([]Tenant, 0, len(rows))
	for _, r := range rows {
		t, ok := parseListRow(r)
		if !ok {
			continue
		}
		t.LastBackup = lastBackup
		out = append(out, t)
	}
	return out, nil
}

// parseListRow turns one tab-separated psql row into a Tenant. Returns
// false if the row is malformed (wrong number of columns), preserving the
// rest of the listing rather than failing the whole call.
func parseListRow(r []string) (Tenant, bool) {
	if len(r) < 4 {
		return Tenant{}, false
	}
	name := strings.TrimSpace(r[0])
	if name == "" {
		return Tenant{}, false
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(r[2]), 10, 64)
	conns, _ := strconv.Atoi(strings.TrimSpace(r[3]))
	return Tenant{
		Name:        name,
		Role:        name,
		Database:    name,
		SecretName:  K8sSecretName(name),
		Owner:       strings.TrimSpace(r[1]),
		SizeBytes:   size,
		Connections: conns,
	}, true
}

// lastClusterBackupTimestamp queries CNPG Backup CRs and returns the most
// recent completed backup's StoppedAt as RFC3339, or "" if none / on error.
// Failures are best-effort: visibility shouldn't block the inventory.
func lastClusterBackupTimestamp(ctx context.Context, d Deps) string {
	if d.K8s == nil || d.K8s.Dynamic == nil || d.Workspace == nil {
		return ""
	}
	backups, err := cnpg.ListBackups(ctx, d.K8s.Dynamic, d.Workspace.Namespace, ClusterName)
	if err != nil {
		return ""
	}
	b := cnpg.LatestCompleted(backups)
	if b.StoppedAt.IsZero() {
		return ""
	}
	return b.StoppedAt.UTC().Format(time.RFC3339)
}
