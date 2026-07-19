package web

import (
	"fmt"
	"sort"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

// ClusterTarget is one independently routed CNPG cluster.
type ClusterTarget struct {
	ID          string
	DisplayName string
	Deps        tenant.Deps
}

// ClusterName returns the target CNPG Cluster resource name.
func (c ClusterTarget) ClusterName() string {
	if c.Deps.Workspace == nil {
		return ""
	}
	return c.Deps.Workspace.ClusterName
}

// Namespace returns the target Kubernetes namespace.
func (c ClusterTarget) Namespace() string {
	if c.Deps.Workspace == nil {
		return ""
	}
	return c.Deps.Workspace.Namespace
}

// ClusterRegistry is immutable after construction and safe to share across
// HTTP requests.
type ClusterRegistry struct {
	Default string
	Targets map[string]ClusterTarget
}

// NewClusterRegistry validates and indexes targets.
func NewClusterRegistry(defaultID string, targets []ClusterTarget) (ClusterRegistry, error) {
	registry := ClusterRegistry{Default: defaultID, Targets: make(map[string]ClusterTarget, len(targets))}
	for _, target := range targets {
		if target.ID == "" {
			return ClusterRegistry{}, fmt.Errorf("cluster target has an empty ID")
		}
		if _, exists := registry.Targets[target.ID]; exists {
			return ClusterRegistry{}, fmt.Errorf("duplicate cluster target %q", target.ID)
		}
		if target.DisplayName == "" {
			target.DisplayName = target.ID
		}
		registry.Targets[target.ID] = target
	}
	if len(registry.Targets) == 0 {
		return ClusterRegistry{}, fmt.Errorf("no cluster targets configured")
	}
	if registry.Default == "" && len(registry.Targets) == 1 {
		for id := range registry.Targets {
			registry.Default = id
		}
	}
	if _, ok := registry.Targets[registry.Default]; !ok {
		return ClusterRegistry{}, fmt.Errorf("default cluster %q is not configured", registry.Default)
	}
	return registry, nil
}

// IDs returns target IDs in deterministic order.
func (r ClusterRegistry) IDs() []string {
	ids := make([]string, 0, len(r.Targets))
	for id := range r.Targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
