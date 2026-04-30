package pg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aberti/cnpg-portal/internal/k8s"
)

// DumpFormatCustom is the pg_dump -Fc magic header prefix.
const DumpFormatCustom = "PGDMP"

// DetectDumpFormat returns "custom" if r starts with PGDMP (after peek), else "sql".
// The returned reader is the same logical stream starting from the beginning (buffered).
func DetectDumpFormat(r io.Reader) (format string, wrapped io.Reader, err error) {
	br := bufio.NewReader(r)
	peek, err := br.Peek(5)
	if err != nil && err != io.EOF && len(peek) == 0 {
		return "", nil, fmt.Errorf("peek dump: %w", err)
	}
	if len(peek) >= 5 && string(peek[:5]) == DumpFormatCustom {
		return "custom", br, nil
	}
	return "sql", br, nil
}

// RestoreCustomFromReader runs pg_restore from a custom-format (-Fc) dump on stdin.
func (c *Conn) RestoreCustomFromReader(ctx context.Context, dstDB string, r io.Reader) error {
	if !IdentSafe(dstDB) {
		return fmt.Errorf("RestoreCustomFromReader: dstDB must be IdentSafe")
	}
	var stderr bytes.Buffer
	err := c.K8s.Exec(ctx, k8s.ExecOptions{
		Namespace: c.Namespace, Pod: c.Pod, Container: c.Container,
		Command: []string{
			"pg_restore", "-U", "postgres",
			"--no-owner", "--no-privileges",
			"--role", dstDB,
			"-d", dstDB,
		},
		Stdin:  r,
		Stderr: &stderr,
	})
	if err != nil {
		return fmt.Errorf("pg_restore: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// RestoreSQLFromReader runs plain SQL from stdin via psql -f -.
func (c *Conn) RestoreSQLFromReader(ctx context.Context, dstDB string, r io.Reader) error {
	if !IdentSafe(dstDB) {
		return fmt.Errorf("RestoreSQLFromReader: dstDB must be IdentSafe")
	}
	args := []string{"psql", "-U", "postgres", "-d", dstDB, "-v", "ON_ERROR_STOP=1", "-f", "-"}
	var stderr bytes.Buffer
	err := c.K8s.Exec(ctx, k8s.ExecOptions{
		Namespace: c.Namespace, Pod: c.Pod, Container: c.Container,
		Command: args,
		Stdin:   r,
		Stderr:  &stderr,
	})
	if err != nil {
		return fmt.Errorf("psql: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
