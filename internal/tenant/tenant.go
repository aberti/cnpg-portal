// Package tenant implements the verb-level operations cnpg-portal exposes
// (Provision, List, Status, Drop, Branch). Verbs are pure Go functions that
// take a Deps bundle so the same code paths drive the CLI and the web UI.
//
// Idempotency is the design rule: every verb is safe to retry against any
// state, so partial failures are recoverable by re-running.
package tenant

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/aberti/cnpg-portal/internal/k8s"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

// Deps is the dependency bundle every verb takes. Constructed once per
// invocation by cmd/cnpgctl and the web handlers.
type Deps struct {
	K8s       *k8s.Client
	PG        *pg.Conn
	Workspace *workspace.Workspace
}

// Tenant is the shared shape returned by Provision/List/Status. Sensitive
// fields (DatabaseURL contains the password) must not be logged.
type Tenant struct {
	Name        string
	Role        string
	Database    string
	SecretName  string
	DatabaseURL string

	// Populated by List/Status; zero on Provision.
	Owner       string
	SizeBytes   int64
	Connections int
	LastBackup  string // RFC3339 timestamp; "" if none

	// Status-only role attributes; zero from List.
	Login           bool
	CreateDB        bool
	Inherit         bool
	ConnectionLimit int // -1 = unlimited (Postgres convention)
}

// FormatBytes renders a byte count using IEC binary units (KiB/MiB/GiB) with
// one decimal of precision. Used by both CLI table rendering and the UI.
func FormatBytes(b int64) string {
	const k = 1024
	switch {
	case b < k:
		return fmt.Sprintf("%d B", b)
	case b < k*k:
		return fmt.Sprintf("%.1f KiB", float64(b)/k)
	case b < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(b)/(k*k))
	default:
		return fmt.Sprintf("%.2f GiB", float64(b)/(k*k*k))
	}
}

// generatePassword returns 32 random bytes encoded as URL-safe base64
// (matching provision-db.sh's openssl rand -base64 24 in spirit but a touch
// stronger and free of `+/` characters that confuse DSN encoding).
func generatePassword() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// buildDatabaseURL builds the in-cluster DSN. It URL-escapes the password
// in case it contains characters DSNs treat specially.
func buildDatabaseURL(role, password, namespace, db string) string {
	host := fmt.Sprintf("pg-primary-rw.%s.svc.cluster.local", namespace)
	return (&url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(role, password),
		Host:   host + ":5432",
		Path:   "/" + db,
	}).String()
}
