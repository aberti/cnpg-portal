package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aberti/cnpg-portal/internal/pg"
)

// SyncOptions controls the destructive overwrite.
type SyncOptions struct {
	// Confirmed must be true for Sync to proceed. The CLI sets this from
	// --yes-i-mean-it; the UI sets it after the user types the destination
	// tenant name in a confirmation field.
	Confirmed bool
}

// Sync overwrites tenant `dst` with the contents of tenant `src`. The
// destination's role and credential Secret are preserved; only its database
// is dropped and recreated, then loaded from `pg_dump -Fc src | pg_restore`.
// Existing apps using dst's DSN keep working post-sync — they just see
// different data.
//
// Steps:
//  1. Validate inputs (IdentSafe, distinct, both DBs exist).
//  2. DROP DATABASE dst WITH (FORCE) — PG 13+ atomically kicks open sessions.
//  3. CREATE DATABASE dst OWNER dst — restores ownership the role expects.
//  4. pg_dump -Fc src | pg_restore --no-owner --no-privileges --role=dst -d dst.
//  5. ensureGrants(dst) — re-applies standard tenant grants on the fresh DB.
//
// Re-running with the same src/dst is safe: the destination is wiped and
// reloaded each time.
func Sync(ctx context.Context, d Deps, src, dst string, opts SyncOptions) error {
	if !pg.DatabaseNameSafe(src) {
		return fmt.Errorf("invalid source name %q", src)
	}
	if !pg.IdentSafe(dst) {
		return fmt.Errorf("invalid destination name %q", dst)
	}
	if src == dst {
		return errors.New("Sync: src and dst must differ")
	}
	if !opts.Confirmed {
		return ErrConfirmationRequired
	}
	if d.PG == nil {
		return errors.New("Sync: PG dep is required")
	}
	logger := slog.With("verb", "sync", "src", src, "dst", dst)

	for _, name := range []string{src, dst} {
		ok, err := databaseExists(ctx, d.PG, name)
		if err != nil {
			return fmt.Errorf("check database %s: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("tenant %q: %w", name, ErrTenantNotFound)
		}
	}

	qdst, err := pg.QuoteIdent(dst)
	if err != nil {
		return err
	}

	// DROP DATABASE ... WITH (FORCE) — PG 13+ atomically terminates open
	// sessions. CNPG runs PG 17 in this fleet; no fallback needed.
	if _, err := d.PG.RunSQL(ctx, "", fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", qdst)); err != nil {
		return fmt.Errorf("drop destination database: %w", err)
	}
	logger.Info("destination database dropped")

	if _, err := d.PG.RunSQL(ctx, "", fmt.Sprintf("CREATE DATABASE %s OWNER %s;", qdst, qdst)); err != nil {
		return fmt.Errorf("recreate destination database: %w", err)
	}
	logger.Info("destination database recreated, starting in-cluster pg_dump | pg_restore")

	if err := d.PG.PgDumpRestore(ctx, src, dst); err != nil {
		return fmt.Errorf("pg_dump | pg_restore %s -> %s: %w", src, dst, err)
	}
	if err := ensureGrants(ctx, d.PG, dst); err != nil {
		return fmt.Errorf("ensure grants after sync: %w", err)
	}
	logger.Info("sync complete")
	return nil
}

// databaseExists reports whether a database named `name` is present in the
// cluster. Cheap pg_database lookup; safe to call on hot paths.
func databaseExists(ctx context.Context, conn *pg.Conn, name string) (bool, error) {
	rows, err := conn.RunQuery(ctx, "", fmt.Sprintf(
		"SELECT 1 FROM pg_database WHERE datname = '%s';", pg.LiteralEscape(name),
	))
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}
