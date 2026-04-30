package tenant

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aberti/cnpg-portal/internal/pg"
)

// statusSQL fetches the per-tenant projection used by the focused view. The
// LEFT JOIN against pg_roles handles the rare case where the database
// exists but the matching role has been removed (orphan DB).
const statusSQL = `SELECT
  d.datname,
  pg_catalog.pg_get_userbyid(d.datdba) AS owner,
  pg_database_size(d.datname) AS size_bytes,
  COALESCE(s.numbackends, 0) AS conns,
  COALESCE(r.rolcanlogin, false) AS can_login,
  COALESCE(r.rolcreatedb, false) AS create_db,
  COALESCE(r.rolinherit, false) AS inherit,
  COALESCE(r.rolconnlimit, -1) AS conn_limit
FROM pg_database d
LEFT JOIN pg_stat_database s ON s.datname = d.datname
LEFT JOIN pg_roles r ON r.rolname = d.datname
WHERE d.datname = '%s'`

// ErrTenantNotFound signals Status couldn't find the requested database.
var ErrTenantNotFound = errors.New("tenant not found")

// Status returns a focused per-tenant view: size, conns, role attributes,
// and the last cluster-wide backup timestamp.
func Status(ctx context.Context, d Deps, app string) (*Tenant, error) {
	if d.PG == nil {
		return nil, errors.New("Status: PG dep is required")
	}
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	rows, err := d.PG.RunQuery(ctx, "", fmt.Sprintf(statusSQL, pg.LiteralEscape(app)))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrTenantNotFound
	}
	r := rows[0]
	if len(r) < 8 {
		return nil, fmt.Errorf("unexpected row width %d", len(r))
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(r[2]), 10, 64)
	conns, _ := strconv.Atoi(strings.TrimSpace(r[3]))
	connLimit, _ := strconv.Atoi(strings.TrimSpace(r[7]))
	return &Tenant{
		Name:            strings.TrimSpace(r[0]),
		Role:            strings.TrimSpace(r[0]),
		Database:        strings.TrimSpace(r[0]),
		SecretName:      K8sSecretName(strings.TrimSpace(r[0])),
		Owner:           strings.TrimSpace(r[1]),
		SizeBytes:       size,
		Connections:     conns,
		Login:           parseBool(r[4]),
		CreateDB:        parseBool(r[5]),
		Inherit:         parseBool(r[6]),
		ConnectionLimit: connLimit,
		LastBackup:      lastClusterBackupTimestamp(ctx, d),
	}, nil
}

// parseBool tolerates psql's "t" / "f" + Go's "true" / "false" forms.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "true", "1", "yes":
		return true
	default:
		return false
	}
}
