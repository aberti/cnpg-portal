// Package sops shells out to the `sops` binary to encrypt/decrypt secret
// files in place. Reimplementing SOPS in Go is out of scope.
//
// Rules of engagement (read from the GitOps WORKSPACE's .sops.yaml at
// workspaceRoot, NOT from a .sops.yaml in this repo — cnpg-portal source
// ships none):
//   - The age private key lives off-cluster at $SOPS_AGE_KEY_FILE (default
//     ~/.sops.age.key). The CLI uses it to encrypt and decrypt.
//   - The age public key is committed in <workspace>/.sops.yaml. SOPS
//     locates it via the cmd.Dir below.
package sops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// EncryptInPlace runs `sops --encrypt --in-place <path>`. Requires `sops`
// on PATH. cwd is set to workspaceRoot so SOPS resolves the .sops.yaml
// recipients via path_regex relative to the repository.
func EncryptInPlace(ctx context.Context, workspaceRoot, path string) error {
	cmd := exec.CommandContext(ctx, "sops", "--encrypt", "--in-place", path)
	cmd.Dir = workspaceRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sops encrypt %s: %w (stderr: %s)", path, err, stderr.String())
	}
	return nil
}

// Decrypt reads `sops --decrypt <path>` to stdout. Used by the CLI to read
// an existing tenant Secret without leaving plaintext on disk.
func Decrypt(ctx context.Context, workspaceRoot, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sops", "--decrypt", path)
	cmd.Dir = workspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sops decrypt %s: %w (stderr: %s)", path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// ErrNotInstalled is returned by CheckAvailable when the `sops` binary is
// not on $PATH. Callers should surface this as actionable installation
// guidance rather than a generic exec error.
var ErrNotInstalled = errors.New("sops not found on PATH (install via mise or system package manager)")

// CheckAvailable reports whether the `sops` binary can be located on PATH.
// Returns ErrNotInstalled if not.
func CheckAvailable() error {
	if _, err := exec.LookPath("sops"); err != nil {
		return ErrNotInstalled
	}
	return nil
}
