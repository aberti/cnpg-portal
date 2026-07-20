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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type workspaceConfig struct {
	CreationRules []struct {
		PathRegex string `yaml:"path_regex"`
		Age       string `yaml:"age"`
	} `yaml:"creation_rules"`
}

// EncryptForWorkspace resolves the age recipient from the workspace's
// matching .sops.yaml rule and encrypts plain entirely in memory.
func EncryptForWorkspace(workspaceRoot, targetPath string, plain []byte) ([]byte, error) {
	configData, err := os.ReadFile(filepath.Join(workspaceRoot, ".sops.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read workspace .sops.yaml: %w", err)
	}
	var config workspaceConfig
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("parse workspace .sops.yaml: %w", err)
	}
	rel, err := filepath.Rel(workspaceRoot, targetPath)
	if err != nil {
		return nil, fmt.Errorf("resolve target path: %w", err)
	}
	rel = filepath.ToSlash(rel)
	for _, rule := range config.CreationRules {
		if rule.PathRegex == "" || rule.Age == "" {
			continue
		}
		re, err := regexp.Compile(rule.PathRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid SOPS path_regex %q: %w", rule.PathRegex, err)
		}
		if !re.MatchString(rel) {
			continue
		}
		recipients := strings.FieldsFunc(rule.Age, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		if len(recipients) != 1 {
			return nil, fmt.Errorf("matched SOPS rule must contain exactly one age recipient")
		}
		return EncryptYAMLForRecipient(plain, strings.TrimSpace(recipients[0]))
	}
	return nil, fmt.Errorf("no .sops.yaml creation rule matches %q", rel)
}

// WriteEncryptedAtomic writes already encrypted data beside the destination
// and atomically renames it into place. Plaintext never touches the workspace.
func WriteEncryptedAtomic(path string, encrypted []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cnpg-portal-encrypted-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(encrypted); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

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
