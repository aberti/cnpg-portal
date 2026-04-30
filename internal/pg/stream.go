package pg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/aberti/cnpg-portal/internal/k8s"
)

// CrossPodDumpRestore streams pg_dump -Fc from src into pg_restore on dst, both
// in-cluster exec sessions. The destination database must already exist.
func CrossPodDumpRestore(ctx context.Context, src, dst *Conn, srcDB, dstDB string) error {
	if !IdentSafe(srcDB) || !IdentSafe(dstDB) {
		return fmt.Errorf("CrossPodDumpRestore: srcDB/dstDB must be IdentSafe")
	}
	if src == nil || dst == nil || src.K8s == nil || dst.K8s == nil {
		return fmt.Errorf("CrossPodDumpRestore: src and dst Conn required")
	}
	pr, pw := io.Pipe()
	var dumpStderr, restoreStderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	dumpErrC := make(chan error, 1)
	restoreErrC := make(chan error, 1)
	go func() {
		defer wg.Done()
		defer func() { _ = pw.Close() }()
		err := src.K8s.Exec(ctx, k8s.ExecOptions{
			Namespace: src.Namespace, Pod: src.Pod, Container: src.Container,
			Command: []string{"pg_dump", "-U", "postgres", "-Fc", "-d", srcDB},
			Stdout:  pw,
			Stderr:  &dumpStderr,
		})
		dumpErrC <- err
	}()
	go func() {
		defer wg.Done()
		err := dst.K8s.Exec(ctx, k8s.ExecOptions{
			Namespace: dst.Namespace, Pod: dst.Pod, Container: dst.Container,
			Command: []string{
				"pg_restore", "-U", "postgres",
				"--no-owner", "--no-privileges",
				"--role", dstDB,
				"-d", dstDB,
			},
			Stdin:  pr,
			Stderr: &restoreStderr,
		})
		restoreErrC <- err
	}()
	dumpErr := <-dumpErrC
	restoreErr := <-restoreErrC
	wg.Wait()
	_ = pr.Close()
	if dumpErr != nil {
		return fmt.Errorf("pg_dump on recovery: %w (stderr: %s)", dumpErr, dumpStderr.String())
	}
	if restoreErr != nil {
		return fmt.Errorf("pg_restore on primary: %w (stderr: %s)", restoreErr, restoreStderr.String())
	}
	return nil
}
