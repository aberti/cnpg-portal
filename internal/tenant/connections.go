package tenant

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aberti/cnpg-portal/internal/pg"
)

// Connection is one row from pg_stat_activity for a tenant database.
type Connection struct {
	PID             int
	User            string
	ApplicationName string
	ClientAddr      string
	State           string
	Age             string
	Query           string
}

const connectionsSQL = `SELECT
  pid,
  usename,
  COALESCE(application_name, ''),
  COALESCE(client_addr::text, ''),
  COALESCE(state, ''),
  (now() - state_change)::text AS age,
  regexp_replace(COALESCE(query, ''), E'[\\n\\t\\r]+', ' ', 'g') AS query
FROM pg_stat_activity
WHERE datname = '%s'
ORDER BY now() - state_change DESC`

// Connections returns current active sessions for one tenant database.
func Connections(ctx context.Context, d Deps, app string) ([]Connection, error) {
	if d.PG == nil {
		return nil, errors.New("Connections: PG dep is required")
	}
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	rows, err := d.PG.RunQuery(ctx, "", fmt.Sprintf(connectionsSQL, pg.LiteralEscape(app)))
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(rows))
	for _, r := range rows {
		if len(r) < 7 {
			continue
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(r[0]))
		out = append(out, Connection{
			PID:             pid,
			User:            strings.TrimSpace(r[1]),
			ApplicationName: strings.TrimSpace(r[2]),
			ClientAddr:      strings.TrimSpace(r[3]),
			State:           strings.TrimSpace(r[4]),
			Age:             strings.TrimSpace(r[5]),
			Query:           strings.TrimSpace(r[6]),
		})
	}
	return out, nil
}

// TerminateConnection asks Postgres to terminate one backend PID.
func TerminateConnection(ctx context.Context, d Deps, app string, pid int) error {
	if d.PG == nil {
		return errors.New("TerminateConnection: PG dep is required")
	}
	if !pg.IdentSafe(app) {
		return fmt.Errorf("invalid tenant name %q", app)
	}
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	_, err := d.PG.RunSQL(ctx, "", fmt.Sprintf(
		"SELECT pg_terminate_backend(%d) FROM pg_stat_activity WHERE datname = '%s' AND pid = %d;",
		pid, pg.LiteralEscape(app), pid,
	))
	return err
}
