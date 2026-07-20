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
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarkerName is the filename whose presence anchors the workspace root.
const MarkerName = ".cnpg-portal-workspace"

// EnvVar holds the absolute path override used when discovery should skip
// CWD-walking.
const EnvVar = "CNPG_PORTAL_WORKSPACE"

// ClusterEnvVar selects a cluster from a multi-cluster workspace for CLI
// commands when --cluster is not provided.
const ClusterEnvVar = "CNPG_PORTAL_CLUSTER"

// DefaultClusterID is used when a legacy, flat marker is loaded.
const DefaultClusterID = "default"

// ErrNotFound signals that no workspace marker was discovered.
var ErrNotFound = errors.New("no .cnpg-portal-workspace marker found (set --workspace or $CNPG_PORTAL_WORKSPACE)")

// ErrClusterNotFound signals that a requested cluster ID is not configured.
var ErrClusterNotFound = errors.New("cluster not found in workspace")

// Workspace describes the resolved workspace root and the conventions inside
// it. Defaults are filled in from project conventions when the marker is
// silent on a key.
type Workspace struct {
	Root        string `yaml:"-"`
	ID          string `yaml:"-"`
	DisplayName string `yaml:"display_name,omitempty"`
	ClusterYAML string `yaml:"cluster_yaml"`
	SecretsDir  string `yaml:"secrets_dir"`
	Namespace   string `yaml:"namespace"`
	Pod         string `yaml:"pod"`
	Container   string `yaml:"container"`

	// ClusterName is the CNPG Cluster CR's name — used for backup, restore,
	// and Cluster status operations. If empty, derived from Pod by stripping
	// the trailing instance index (e.g. `postgres-main-1` → `postgres-main`).
	ClusterName string `yaml:"cluster_name"`

	// ServiceName is the *stable* DNS prefix used in app connection strings
	// (`<ServiceName>-rw.<Namespace>.svc.cluster.local`). For deployments that
	// use the alias-Service pattern (recommended; survives cluster replacements
	// without rewriting app secrets) this is e.g. `postgres-main` regardless of
	// what the underlying CNPG cluster is named. If empty, defaults to
	// ClusterName — matching CNPG's auto-created `<cluster>-{rw,ro,r}`
	// services so single-cluster, never-upgraded setups work out of the box.
	ServiceName string `yaml:"service_name"`

	// SecretPrefix prevents credentials for equal tenant names from colliding
	// when multiple CNPG Clusters share a Kubernetes namespace. It is optional
	// for compatibility with existing clusters and their Secret names.
	SecretPrefix string `yaml:"secret_prefix,omitempty"`
}

// Catalog is the resolved set of CNPG targets declared by one workspace.
// Each target keeps the existing Workspace shape so tenant verbs remain
// unaware of multi-cluster routing.
type Catalog struct {
	Root           string
	DefaultCluster string
	Clusters       map[string]*Workspace
}

type markerFile struct {
	Workspace      `yaml:",inline"`
	DefaultCluster string                `yaml:"default_cluster,omitempty"`
	Clusters       map[string]*Workspace `yaml:"clusters,omitempty"`
}

// ClusterYAMLPath resolves to the absolute path of the CNPG Cluster manifest.
func (w Workspace) ClusterYAMLPath() string { return filepath.Join(w.Root, w.ClusterYAML) }

// SecretName returns the namespace-scoped Kubernetes Secret used for app.
func (w Workspace) SecretName(app string) string {
	base := dns1123Basename(app)
	if w.SecretPrefix != "" {
		base = dns1123Basename(w.SecretPrefix) + "-" + base
	}
	return base + "-pg-credentials"
}

// SecretPath returns the absolute path where the SOPS-encrypted Secret for
// app should be written. The filename uses the same DNS-1123 normalization
// as the in-cluster Secret name (underscores → hyphens), so the file on
// disk and the K8s Secret it decrypts to share the same basename. Without
// this, a tenant like `laravel_user` would end up with file
// `laravel_user-pg-credentials.sops.yaml` (underscore) but Secret
// metadata.name `laravel-user-pg-credentials` (hyphen) — same content,
// different basenames, breaking idempotent re-discovery.
func (w Workspace) SecretPath(app string) string {
	return filepath.Join(w.Root, w.SecretsDir, w.SecretName(app)+".sops.yaml")
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
	catalog, err := FindCatalog(override)
	if err != nil {
		return nil, err
	}
	return catalog.Select("")
}

// FindCluster resolves a marker and selects clusterID. An empty ID falls back
// to $CNPG_PORTAL_CLUSTER, then the marker's default_cluster.
func FindCluster(override, clusterID string) (*Workspace, error) {
	catalog, err := FindCatalog(override)
	if err != nil {
		return nil, err
	}
	if clusterID == "" {
		clusterID = os.Getenv(ClusterEnvVar)
	}
	return catalog.Select(clusterID)
}

// FindCatalog resolves and parses the workspace marker using Find's existing
// discovery precedence.
func FindCatalog(override string) (*Catalog, error) {
	if override == "" {
		override = os.Getenv(EnvVar)
	}
	if override != "" {
		return loadCatalog(override)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, MarkerName)); err == nil {
			return loadCatalog(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

func loadCatalog(root string) (*Catalog, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(abs, MarkerName)
	data, err := os.ReadFile(marker)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", marker, err)
	}
	markerConfig := markerFile{}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &markerConfig); err != nil {
			return nil, fmt.Errorf("parse %s: %w", marker, err)
		}
	}

	catalog := &Catalog{Root: abs, Clusters: map[string]*Workspace{}}
	if len(markerConfig.Clusters) == 0 {
		w := markerConfig.Workspace
		w.Root = abs
		w.ID = DefaultClusterID
		w.DisplayName = "Default cluster"
		w.applyDefaults()
		catalog.DefaultCluster = DefaultClusterID
		catalog.Clusters[DefaultClusterID] = &w
		return catalog, nil
	}

	catalog.DefaultCluster = markerConfig.DefaultCluster
	if catalog.DefaultCluster == "" && len(markerConfig.Clusters) == 1 {
		for id := range markerConfig.Clusters {
			catalog.DefaultCluster = id
		}
	}
	if catalog.DefaultCluster == "" {
		return nil, fmt.Errorf("parse %s: default_cluster is required when multiple clusters are configured", marker)
	}

	for id, config := range markerConfig.Clusters {
		if !validClusterID(id) {
			return nil, fmt.Errorf("parse %s: invalid cluster ID %q (use lowercase DNS-1123 labels)", marker, id)
		}
		if config == nil {
			return nil, fmt.Errorf("parse %s: cluster %q has no configuration", marker, id)
		}
		w := *config
		w.Root = abs
		w.ID = id
		if w.DisplayName == "" {
			w.DisplayName = id
		}
		w.applyDefaults()
		if err := validateWorkspace(w); err != nil {
			return nil, fmt.Errorf("parse %s: cluster %q: %w", marker, id, err)
		}
		catalog.Clusters[id] = &w
	}
	if _, ok := catalog.Clusters[catalog.DefaultCluster]; !ok {
		return nil, fmt.Errorf("parse %s: default_cluster %q is not declared", marker, catalog.DefaultCluster)
	}
	return catalog, nil
}

// Select returns the requested target, defaulting to DefaultCluster.
func (c *Catalog) Select(id string) (*Workspace, error) {
	if c == nil {
		return nil, ErrClusterNotFound
	}
	if id == "" {
		id = c.DefaultCluster
	}
	w, ok := c.Clusters[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrClusterNotFound, id)
	}
	return w, nil
}

// IDs returns configured cluster IDs in deterministic order.
func (c *Catalog) IDs() []string {
	ids := make([]string, 0, len(c.Clusters))
	for id := range c.Clusters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func validateWorkspace(w Workspace) error {
	if err := validateRelativePath("cluster_yaml", w.ClusterYAML, true); err != nil {
		return err
	}
	if err := validateRelativePath("secrets_dir", w.SecretsDir, false); err != nil {
		return err
	}
	if w.Pod == "" {
		return errors.New("pod is required")
	}
	if w.ClusterName == "" {
		return errors.New("cluster_name is required or must be derivable from pod")
	}
	if w.ServiceName == "" {
		return errors.New("service_name is required or must default from cluster_name")
	}
	if w.SecretPrefix != "" && !validClusterID(w.SecretPrefix) {
		return fmt.Errorf("invalid secret_prefix %q (use a lowercase DNS-1123 label)", w.SecretPrefix)
	}
	return nil
}

func validateRelativePath(field, value string, file bool) error {
	if value == "" {
		if file {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if filepath.IsAbs(value) {
		return fmt.Errorf("%s must be relative to the workspace root", field)
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s escapes the workspace root", field)
	}
	return nil
}

func validClusterID(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
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
