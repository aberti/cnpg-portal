// Package pg sends SQL statements to the CNPG primary pod via Kubernetes
// remote-exec. We deliberately do *not* keep a long-lived database/sql
// connection: connecting via the cluster's internal Service requires either
// a port-forward or in-cluster networking, and either way the cnpgctl CLI
// already needs k8s API access. Using `kubectl exec psql` keeps both
// transports identical.
//
// Identifier safety: cnpg-portal strictly validates app names with
// IdentSafe before they reach SQL, so identifiers can be inlined as
// double-quoted strings without an escaping ceremony. Literals (passwords,
// strings) go through LiteralEscape.
package pg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/aberti/cnpg-portal/internal/k8s"
)

// Conn carries the K8s coordinates used to exec into the CNPG primary pod.
type Conn struct {
	K8s       *k8s.Client
	Namespace string
	Pod       string
	Container string // empty → default container of the pod
}

// SuperuserDB is the database psql connects to when none is specified.
const SuperuserDB = "postgres"

var (
	identSafePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	databaseNameSafePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
)

// IdentSafe reports whether s is a Postgres identifier we will accept
// without further quoting machinery. We restrict to lowercase ASCII so the
// identifier survives both `EVS_DB` quirks and double-quoted SQL contexts.
func IdentSafe(s string) bool {
	return identSafePattern.MatchString(s)
}

// DatabaseNameSafe reports whether s is an existing database name that can
// be passed as one argv value or escaped SQL literal. Unlike new portal
// roles, externally managed databases may contain hyphens.
func DatabaseNameSafe(s string) bool {
	return databaseNameSafePattern.MatchString(s)
}

// QuoteIdent wraps a Postgres identifier in double quotes after a defensive
// IdentSafe check. Returns an error rather than producing dangerous SQL.
func QuoteIdent(s string) (string, error) {
	if !IdentSafe(s) {
		return "", fmt.Errorf("unsafe identifier: %q (must match [a-z][a-z0-9_]{0,62})", s)
	}
	return `"` + s + `"`, nil
}

// LiteralEscape returns s formatted as a Postgres string literal, escaping
// embedded single quotes by doubling. Use only inside a single-quoted SQL
// literal context: caller should write `'<LiteralEscape(s)>'`.
func LiteralEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// RunSQL sends sqlScript to psql via stdin against database db (defaulting
// to the superuser database). Output is psql's tuples-only stdout (`-At`).
// Errors capture stderr for debugging.
func (c *Conn) RunSQL(ctx context.Context, db, sqlScript string) (string, error) {
	if db == "" {
		db = SuperuserDB
	}
	args := []string{"psql", "-U", "postgres", "-d", db, "-At", "--set=ON_ERROR_STOP=1"}
	return c.exec(ctx, args, strings.NewReader(sqlScript))
}

// RunQuery is RunSQL plus parsing of `psql -At -F<TAB>` output into rows.
// Rows are split on '\n', columns on '\t'. Empty trailing line is dropped.
func (c *Conn) RunQuery(ctx context.Context, db, query string) ([][]string, error) {
	if db == "" {
		db = SuperuserDB
	}
	args := []string{"psql", "-U", "postgres", "-d", db, "-At", "-F", "\t", "--set=ON_ERROR_STOP=1", "-c", query}
	out, err := c.exec(ctx, args, nil)
	if err != nil {
		return nil, err
	}
	rows := strings.Split(strings.TrimRight(out, "\n"), "\n")
	parsed := make([][]string, 0, len(rows))
	for _, r := range rows {
		if r == "" {
			continue
		}
		parsed = append(parsed, strings.Split(r, "\t"))
	}
	return parsed, nil
}

// PgDumpRestore runs `pg_dump -Fc -d src | pg_restore` into dst inside the pod.
// --no-privileges skips GRANT/ALTER DEFAULT PRIVILEGES replay from the dump; those
// often reference the source role or postgres and fail when cloning to a new tenant.
// Callers re-apply grants for dst after a successful restore (see tenant.Branch).
func (c *Conn) PgDumpRestore(ctx context.Context, src, dst string) error {
	if !DatabaseNameSafe(src) || !IdentSafe(dst) {
		return fmt.Errorf("PgDumpRestore: src must be DatabaseNameSafe and dst must be IdentSafe")
	}
	pipe := fmt.Sprintf(
		"pg_dump -U postgres -Fc -d %q | pg_restore -U postgres --no-owner --no-privileges --role=%q -d %q",
		src, dst, dst,
	)
	_, err := c.exec(ctx, []string{"bash", "-c", pipe}, nil)
	return err
}

// PgDump streams pg_dump -Fc <db> output to w. Suitable for download flows.
func (c *Conn) PgDump(ctx context.Context, db string, w io.Writer) error {
	if !DatabaseNameSafe(db) {
		return fmt.Errorf("PgDump: db must be DatabaseNameSafe")
	}
	var stderr bytes.Buffer
	err := c.K8s.Exec(ctx, k8s.ExecOptions{
		Namespace: c.Namespace, Pod: c.Pod, Container: c.Container,
		Command: []string{"pg_dump", "-U", "postgres", "-Fc", "-d", db},
		Stdout:  w,
		Stderr:  &stderr,
	})
	if err != nil {
		return fmt.Errorf("pg_dump %s: %w (stderr: %s)", db, err, stderr.String())
	}
	return nil
}

func (c *Conn) exec(ctx context.Context, command []string, stdin io.Reader) (string, error) {
	var stdout, stderr bytes.Buffer
	err := c.K8s.Exec(ctx, k8s.ExecOptions{
		Namespace: c.Namespace, Pod: c.Pod, Container: c.Container,
		Command: command,
		Stdin:   stdin,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	if err != nil {
		return stdout.String(), fmt.Errorf("psql: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
