// Package connstr renders a tenant's credentials as connection strings in
// the formats apps actually consume: psql URL, Prisma URL, libpq key=value,
// JDBC, plain env block, and a copy-paste psql command.
//
// The renderers are pure functions of Inputs — no I/O, no globals. Format
// IDs are stable; new audiences are added by appending to All.
package connstr

import (
	"fmt"
	"net/url"
	"strings"
)

// Inputs is the credential bundle every renderer reads. Built by the caller
// from a tenant secret + cluster coordinates.
type Inputs struct {
	Host     string // pg-primary-rw.pg.svc.cluster.local
	Port     int    // 5432
	Database string
	User     string
	Password string
}

// Format describes one rendering audience. Render is required; Description
// is shown to the user under the format label.
type Format struct {
	ID          string
	Label       string
	Description string
	Render      func(Inputs) string
}

// All is the canonical, ordered list rendered on the tenant detail page.
// Order is the same on the page so the most common cases (URL, Prisma)
// appear first.
var All = []Format{
	{
		ID:          "url",
		Label:       "URL (12-factor)",
		Description: "Generic postgres URL — works for most clients (Go, Python, Node libpq-backed drivers).",
		Render:      renderURL,
	},
	{
		ID:          "url-prisma",
		Label:       "Prisma DATABASE_URL",
		Description: "Prisma adds ?schema=public and prefers sslmode=require — paste verbatim into .env.",
		Render:      renderPrismaURL,
	},
	{
		ID:          "libpq",
		Label:       "libpq key=value",
		Description: "psql, libpq, and tools that take a conninfo string. Spaces, no shell quoting.",
		Render:      renderLibpq,
	},
	{
		ID:          "jdbc",
		Label:       "JDBC",
		Description: "JVM apps (Spring, JDBI, Flyway). Username/password are query params.",
		Render:      renderJDBC,
	},
	{
		ID:          "env",
		Label:       ".env block",
		Description: "Five PG* env vars — drop into a Compose / k8s Deployment env block.",
		Render:      renderEnv,
	},
	{
		ID:          "psql",
		Label:       "psql one-liner",
		Description: "Copy-paste shell command. Password passed via PGPASSWORD so it doesn't appear in argv.",
		Render:      renderPsql,
	},
}

// Find returns the named format, or false if no such ID is registered.
func Find(id string) (Format, bool) {
	for _, f := range All {
		if f.ID == id {
			return f, true
		}
	}
	return Format{}, false
}

func renderURL(in Inputs) string {
	u := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(in.User, in.Password),
		Host:   fmt.Sprintf("%s:%d", in.Host, in.Port),
		Path:   "/" + in.Database,
	}
	return u.String()
}

func renderPrismaURL(in Inputs) string {
	u := url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(in.User, in.Password),
		Host:     fmt.Sprintf("%s:%d", in.Host, in.Port),
		Path:     "/" + in.Database,
		RawQuery: "schema=public&sslmode=require",
	}
	return u.String()
}

func renderLibpq(in Inputs) string {
	parts := []string{
		"host=" + in.Host,
		fmt.Sprintf("port=%d", in.Port),
		"dbname=" + in.Database,
		"user=" + in.User,
		"password=" + libpqEscape(in.Password),
	}
	return strings.Join(parts, " ")
}

func renderJDBC(in Inputs) string {
	q := url.Values{
		"user":     []string{in.User},
		"password": []string{in.Password},
	}
	return fmt.Sprintf("jdbc:postgresql://%s:%d/%s?%s",
		in.Host, in.Port, in.Database, q.Encode())
}

func renderEnv(in Inputs) string {
	return strings.Join([]string{
		"PGHOST=" + in.Host,
		fmt.Sprintf("PGPORT=%d", in.Port),
		"PGDATABASE=" + in.Database,
		"PGUSER=" + in.User,
		"PGPASSWORD=" + shQuote(in.Password),
	}, "\n")
}

func renderPsql(in Inputs) string {
	return fmt.Sprintf("PGPASSWORD=%s psql -h %s -p %d -U %s %s",
		shQuote(in.Password), in.Host, in.Port, in.User, in.Database)
}

// libpqEscape wraps a password in single quotes if it contains spaces, '\\',
// or '\”. Inside the wrapper, ' and \ are backslash-escaped per the
// libpq conninfo rules.
func libpqEscape(pw string) string {
	if !strings.ContainsAny(pw, " '\\") {
		return pw
	}
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(pw) + "'"
}

// shQuote wraps a string in POSIX single quotes. Single quotes inside are
// emitted as the four-character idiom '\” so the result is safe to paste
// into bash/zsh/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
