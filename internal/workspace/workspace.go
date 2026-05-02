// Package workspace locates and reads the .cnpg-portal-workspace marker
// file at the root of the GitOps workspace repository. It tells
// cnpg-portal where to find cluster.yaml, where SOPS-encrypted secrets live,
// and which CNPG namespace + pod to drive.
//
// Discovery order:
//  1. The --workspace flag (passed by cobra into Find).
//  2. $CNPG_PORTAL_WORKSPACE.
//  3. Walk up from CWD until a .cnpg-portal-workspace file is found.
//
// Returns ErrNotFound if none of those resolve to an existing marker file.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarkerName is the filename whose presence anchors the workspace root.
const MarkerName = ".cnpg-portal-workspace"

// EnvVar holds the absolute path override used when discovery should skip
// CWD-walking.
const EnvVar = "CNPG_PORTAL_WORKSPACE"

// ErrNotFound signals that no workspace marker was discovered.
var ErrNotFound = errors.New("no .cnpg-portal-workspace marker found (set --workspace or $CNPG_PORTAL_WORKSPACE)")

// Workspace describes the resolved workspace root and the conventions inside
// it. Defaults are filled in from project conventions when the marker is
// silent on a key.
type Workspace struct {
	Root        string `yaml:"-"`
	ClusterYAML string `yaml:"cluster_yaml"`
	SecretsDir  string `yaml:"secrets_dir"`
	Namespace   string `yaml:"namespace"`
	Pod         string `yaml:"pod"`
	Container   string `yaml:"container"`

	// ClusterName is the CNPG Cluster CR's name — used for backup, restore,
	// and Cluster status operations. If empty, derived from Pod by stripping
	// the trailing instance index (e.g. `pg-primary-17-1` → `pg-primary-17`).
	ClusterName string `yaml:"cluster_name"`

	// ServiceName is the *stable* DNS prefix used in app connection strings
	// (`<ServiceName>-rw.<Namespace>.svc.cluster.local`). For deployments that
	// use the alias-Service pattern (recommended; survives PG-major upgrades
	// without rewriting app secrets) this is e.g. `pg-primary` regardless of
	// what the underlying CNPG cluster is named. If empty, defaults to
	// ClusterName — matching CNPG's auto-created `<cluster>-{rw,ro,r}`
	// services so single-cluster, never-upgraded setups work out of the box.
	ServiceName string `yaml:"service_name"`
}

// ClusterYAMLPath resolves to the absolute path of the CNPG Cluster manifest.
func (w Workspace) ClusterYAMLPath() string { return filepath.Join(w.Root, w.ClusterYAML) }

// SecretPath returns the absolute path where the SOPS-encrypted Secret for
// app should be written. The filename uses the same DNS-1123 normalization
// as the in-cluster Secret name (underscores → hyphens), so the file on
// disk and the K8s Secret it decrypts to share the same basename. Without
// this, a tenant like `laravel_user` would end up with file
// `laravel_user-pg-credentials.sops.yaml` (underscore) but Secret
// metadata.name `laravel-user-pg-credentials` (hyphen) — same content,
// different basenames, breaking idempotent re-discovery.
func (w Workspace) SecretPath(app string) string {
	return filepath.Join(w.Root, w.SecretsDir, dns1123Basename(app)+"-pg-credentials.sops.yaml")
}

// dns1123Basename mirrors tenant.dns1123Label without the import cycle.
// Lowercase, underscore→hyphen, strip non-alnum-hyphen, collapse runs.
func dns1123Basename(app string) string {
	s := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(app)), "_", "-")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	s = b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "app"
	}
	return s
}

// Find resolves a workspace using the precedence above. If override is
// non-empty it is used directly without walking; missing marker file is still
// an error.
func Find(override string) (*Workspace, error) {
	if override == "" {
		override = os.Getenv(EnvVar)
	}
	if override != "" {
		return load(override)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, MarkerName)); err == nil {
			return load(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

func load(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(abs, MarkerName)
	data, err := os.ReadFile(marker)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", marker, err)
	}
	w := Workspace{Root: abs}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("parse %s: %w", marker, err)
		}
	}
	w.applyDefaults()
	return &w, nil
}

func (w *Workspace) applyDefaults() {
	// Universal CNPG conventions; deployment-specific values (cluster_yaml
	// path + primary pod name) MUST be set in the marker file.
	if w.SecretsDir == "" {
		w.SecretsDir = "secrets/pg"
	}
	if w.Namespace == "" {
		w.Namespace = "pg"
	}
	if w.Container == "" {
		w.Container = "postgres"
	}
	// CNPG primary pods are named `<cluster>-<index>`. If the marker only
	// declares `pod`, recover ClusterName by trimming the trailing index.
	if w.ClusterName == "" && w.Pod != "" {
		if i := strings.LastIndex(w.Pod, "-"); i > 0 {
			suffix := w.Pod[i+1:]
			allDigits := suffix != ""
			for _, r := range suffix {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				w.ClusterName = w.Pod[:i]
			}
		}
	}
	// Stable-alias decoupling is opt-in. If service_name isn't set, default
	// to ClusterName so callers get the CNPG-auto Service (`<cluster>-rw`).
	if w.ServiceName == "" {
		w.ServiceName = w.ClusterName
	}
}
