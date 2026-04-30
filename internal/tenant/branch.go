package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aberti/cnpg-portal/internal/pg"
)

// DefaultBranchDestination returns a UI default destination name: src + "_dev",
// shortened when necessary so the result stays IdentSafe and ≤ 63 characters.
func DefaultBranchDestination(src string) string {
	if !pg.IdentSafe(src) {
		return ""
	}
	const suffix = "_dev"
	maxPrefix := 63 - len(suffix)
	base := src
	if len(base) > maxPrefix {
		base = base[:maxPrefix]
	}
	cand := base + suffix
	if pg.IdentSafe(cand) {
		return cand
	}
	for _, suf := range []string{"_d", "_b", "_2"} {
		mp := 63 - len(suf)
		b := src
		if len(b) > mp {
			b = b[:mp]
		}
		c := b + suf
		if pg.IdentSafe(c) {
			return c
		}
	}
	return base + "_d"
}

// Branch creates a new tenant `dst` whose database content is a one-shot
// clone of `src`. Implementation:
//  1. Provision dst (idempotent — reuses password if dst already exists).
//  2. Run pg_dump -Fc on src piped into pg_restore on dst, all inside the
//     CNPG primary pod. No S3 round-trip; finishes in seconds at our
//     2 GB scale per ADR-0001.
//
// Re-running Branch with the same src/dst is safe: Provision is idempotent,
// and pg_restore loads into the existing schema (objects already present
// will produce errors that pg_restore tolerates by default — caller can
// inspect the returned Tenant.Database afterward).
func Branch(ctx context.Context, d Deps, src, dst string) (*Tenant, error) {
	if !pg.IdentSafe(src) {
		return nil, fmt.Errorf("invalid source name %q", src)
	}
	if !pg.IdentSafe(dst) {
		return nil, fmt.Errorf("invalid destination name %q", dst)
	}
	if src == dst {
		return nil, errors.New("Branch: src and dst must differ")
	}
	if d.PG == nil {
		return nil, errors.New("Branch: PG dep is required")
	}
	logger := slog.With("verb", "branch", "src", src, "dst", dst)

	t, err := Provision(ctx, d, dst)
	if err != nil {
		return nil, fmt.Errorf("provision %s: %w", dst, err)
	}
	logger.Info("destination tenant ready, starting in-cluster pg_dump | pg_restore")

	if err := d.PG.PgDumpRestore(ctx, src, dst); err != nil {
		return t, fmt.Errorf("pg_dump | pg_restore %s -> %s: %w", src, dst, err)
	}
	// Restore skips privileges (--no-privileges); re-apply standard tenant grants
	// on the destination DB so dst matches Provision semantics after clone.
	if err := ensureGrants(ctx, d.PG, dst); err != nil {
		return t, fmt.Errorf("ensure grants after branch: %w", err)
	}
	logger.Info("branch complete")
	return t, nil
}
