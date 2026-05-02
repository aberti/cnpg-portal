// Package cnpg projects CloudNativePG CRD state into the small subset of
// fields cnpg-portal renders. We use the dynamic client (rather than typed
// clients generated from the CNPG repo) to avoid pulling the full CNPG
// module graph into our build.
package cnpg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// PhaseFailed marks a CNPG Backup that the operator gave up on.
const PhaseFailed = "failed"

// ErrBackupFailed signals that Wait observed the Backup transition to a
// terminal failed state. Callers can use errors.Is to discriminate from
// transient API errors.
var ErrBackupFailed = errors.New("cnpg backup failed")

var backupGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "backups",
}

// PhaseCompleted marks a CNPG Backup that finished successfully.
const PhaseCompleted = "completed"

// Backup is a flattened view of a CNPG Backup CR. Only fields used by the
// CLI / UI are surfaced.
type Backup struct {
	Name      string
	Cluster   string    // .spec.cluster.name
	Phase     string    // .status.phase: "completed" | "running" | "failed" | ...
	StartedAt time.Time // zero if not yet started
	StoppedAt time.Time // zero if not yet finished
	DestPath  string    // .status.destinationPath
}

// ListBackups returns CNPG Backup CRs in the namespace, optionally filtered
// to backups belonging to clusterName when non-empty. Sorted newest first
// by StartedAt.
func ListBackups(ctx context.Context, dyn dynamic.Interface, namespace, clusterName string) ([]Backup, error) {
	list, err := dyn.Resource(backupGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list cnpg backups: %w", err)
	}
	out := make([]Backup, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, parseBackup(&list.Items[i]))
	}
	if clusterName != "" {
		filtered := out[:0]
		for _, b := range out {
			if b.Cluster == clusterName {
				filtered = append(filtered, b)
			}
		}
		out = filtered
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// LatestCompleted returns the most recent Backup with Phase == "completed",
// or a zero-valued Backup if none. Input is assumed to already be sorted
// newest first (as ListBackups returns).
func LatestCompleted(backups []Backup) Backup {
	for _, b := range backups {
		if b.Phase == PhaseCompleted {
			return b
		}
	}
	return Backup{}
}

// Trigger creates a new on-demand CNPG Backup CR targeting clusterName in
// the named namespace. The returned Backup is the freshly created resource;
// callers typically pass it to Wait to block until the operator finishes.
//
// Labels: every CR cnpg-portal creates is tagged
// `app.kubernetes.io/managed-by=cnpg-portal` plus
// `cnpg-portal/triggered-by=<triggeredBy>` so audit queries against
// the cluster can attribute the action.
func Trigger(ctx context.Context, dyn dynamic.Interface, namespace, clusterName, triggeredBy string) (*Backup, error) {
	name := fmt.Sprintf("ondemand-%s-%d", triggeredBy, time.Now().UTC().Unix())
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": backupGVR.Group + "/" + backupGVR.Version,
			"kind":       "Backup",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "cnpg-portal",
					"cnpg-portal/triggered-by":     triggeredBy,
				},
			},
			"spec": map[string]any{
				"cluster": map[string]any{"name": clusterName},
				"method":  "barmanObjectStore",
			},
		},
	}
	created, err := dyn.Resource(backupGVR).Namespace(namespace).
		Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create cnpg backup %s/%s: %w", namespace, name, err)
	}
	b := parseBackup(created)
	return &b, nil
}

// Wait polls the named Backup CR until its phase reaches a terminal state
// (completed or failed) or ctx expires. Poll interval is fixed at 2s — CNPG
// backups against a 2 GB cluster typically finish in 30–90s, so the cost is
// negligible. Returns ErrBackupFailed wrapping the final Backup on failure.
func Wait(ctx context.Context, dyn dynamic.Interface, namespace, name string) (*Backup, error) {
	const pollEvery = 2 * time.Second
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		got, err := dyn.Resource(backupGVR).Namespace(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get cnpg backup %s/%s: %w", namespace, name, err)
		}
		b := parseBackup(got)
		switch b.Phase {
		case PhaseCompleted:
			return &b, nil
		case PhaseFailed:
			return &b, ErrBackupFailed
		}
		select {
		case <-ctx.Done():
			return &b, ctx.Err()
		case <-ticker.C:
		}
	}
}

// parseBackup is the shared projection used by ListBackups; isolated for tests.
func parseBackup(item *unstructured.Unstructured) Backup {
	b := Backup{Name: item.GetName()}
	b.Cluster, _, _ = unstructured.NestedString(item.Object, "spec", "cluster", "name")
	b.Phase, _, _ = unstructured.NestedString(item.Object, "status", "phase")
	b.DestPath, _, _ = unstructured.NestedString(item.Object, "status", "destinationPath")
	if v, ok, _ := unstructured.NestedString(item.Object, "status", "startedAt"); ok {
		b.StartedAt, _ = time.Parse(time.RFC3339, v)
	}
	if v, ok, _ := unstructured.NestedString(item.Object, "status", "stoppedAt"); ok {
		b.StoppedAt, _ = time.Parse(time.RFC3339, v)
	}
	return b
}
