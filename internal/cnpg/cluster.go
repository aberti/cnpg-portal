package cnpg

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var clusterGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

// ClusterPodLabelKey is the label CNPG sets on Postgres pods for a Cluster.
const ClusterPodLabelKey = "cnpg.io/cluster"

// GetCluster returns the CNPG Cluster CR as unstructured.
func GetCluster(ctx context.Context, dyn dynamic.Interface, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := dyn.Resource(clusterGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get cluster %s/%s: %w", namespace, name, err)
	}
	return obj, nil
}

// CreateRecoveryCluster provisions a new Cluster that bootstraps from the
// Barman Cloud plugin object store used by baseCluster. backupID selects the
// base backup to restore from; optional targetTime adds PITR on top.
func CreateRecoveryCluster(ctx context.Context, dyn dynamic.Interface, namespace, newClusterName, backupID string, baseCluster *unstructured.Unstructured, targetTime *time.Time) error {
	if baseCluster == nil {
		return fmt.Errorf("CreateRecoveryCluster: baseCluster required")
	}
	sourceName := baseCluster.GetName()
	if sourceName == "" {
		return fmt.Errorf("base cluster missing metadata.name")
	}
	if strings.TrimSpace(backupID) == "" {
		return fmt.Errorf("backupID is required for plugin recovery")
	}
	imageName, _, err := unstructured.NestedString(baseCluster.Object, "spec", "imageName")
	if err != nil || imageName == "" {
		return fmt.Errorf("base cluster missing spec.imageName")
	}
	storage, found, err := unstructured.NestedMap(baseCluster.Object, "spec", "storage")
	if err != nil || !found || len(storage) == 0 {
		return fmt.Errorf("base cluster missing spec.storage")
	}
	objectStoreName, err := barmanObjectStoreName(baseCluster)
	if err != nil {
		return err
	}

	recovery := map[string]any{
		"source":   sourceName,
		"database": "postgres",
		"owner":    "postgres",
	}
	recoveryTarget := map[string]any{
		"backupID": backupID,
	}
	if targetTime != nil {
		// CNPG accepts ISO-like timestamps for recoveryTarget.targetTime.
		recoveryTarget["targetTime"] = targetTime.UTC().Format(time.RFC3339)
		recoveryTarget["targetAction"] = "promote"
	}
	recovery["recoveryTarget"] = recoveryTarget

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": clusterGVR.Group + "/" + clusterGVR.Version,
			"kind":       "Cluster",
			"metadata": map[string]any{
				"name":      newClusterName,
				"namespace": namespace,
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "cnpg-portal",
				},
			},
			"spec": map[string]any{
				"instances": int64(1),
				"imageName": imageName,
				"storage":   storage,
				"bootstrap": map[string]any{
					"recovery": recovery,
				},
				"externalClusters": []any{
					map[string]any{
						"name": sourceName,
						"plugin": map[string]any{
							"name":    "barman-cloud.cloudnative-pg.io",
							"enabled": true,
							"parameters": map[string]any{
								"barmanObjectName": objectStoreName,
								"serverName":       sourceName,
							},
						},
					},
				},
			},
		},
	}
	_, err = dyn.Resource(clusterGVR).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create recovery cluster %s/%s: %w", namespace, newClusterName, err)
	}
	return nil
}

func barmanObjectStoreName(cluster *unstructured.Unstructured) (string, error) {
	plugins, found, err := unstructured.NestedSlice(cluster.Object, "spec", "plugins")
	if err != nil || !found {
		return "", fmt.Errorf("base cluster missing spec.plugins barman-cloud entry")
	}
	for _, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(plugin, "name")
		if name != "barman-cloud.cloudnative-pg.io" {
			continue
		}
		objectStore, _, _ := unstructured.NestedString(plugin, "parameters", "barmanObjectName")
		if objectStore == "" {
			return "", fmt.Errorf("barman-cloud plugin missing parameters.barmanObjectName")
		}
		return objectStore, nil
	}
	return "", fmt.Errorf("base cluster missing barman-cloud plugin")
}

// DeleteCluster removes a CNPG Cluster CR (PVCs follow operator cleanup rules).
func DeleteCluster(ctx context.Context, dyn dynamic.Interface, namespace, name string) error {
	err := dyn.Resource(clusterGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete cluster %s/%s: %w", namespace, name, err)
	}
	return nil
}

// PrimaryPodName resolves the running primary instance pod for the cluster.
// Filters by cnpg.io/podRole=instance so we never pick up CNPG bootstrap-job
// or full-recovery pods, which share the cluster label but expose a
// different container set (no `postgres`).
func PrimaryPodName(ctx context.Context, cs kubernetes.Interface, namespace, clusterName string) (string, error) {
	list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ClusterPodLabelKey + "=" + clusterName + ",cnpg.io/podRole=instance",
	})
	if err != nil {
		return "", fmt.Errorf("list pods for cluster %q: %w", clusterName, err)
	}
	for i := range list.Items {
		p := &list.Items[i]
		if p.Status.Phase == corev1.PodRunning && podContainerReady(p) {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no running instance pod found for cluster %q", clusterName)
}

func podContainerReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// WaitPrimaryPod polls until PrimaryPodName succeeds or ctx expires.
func WaitPrimaryPod(ctx context.Context, cs kubernetes.Interface, namespace, clusterName string, pollEvery time.Duration) (string, error) {
	try := func() (string, error) { return PrimaryPodName(ctx, cs, namespace, clusterName) }
	if name, err := try(); err == nil && name != "" {
		return name, nil
	}
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait primary pod %q: %w", clusterName, ctx.Err())
		case <-t.C:
			if name, err := try(); err == nil && name != "" {
				return name, nil
			}
		}
	}
}

// BackupObjectNameOK validates a Kubernetes DNS subdomain name for Backup CRs.
func BackupObjectNameOK(name string) bool {
	return len(validation.IsDNS1123Subdomain(name)) == 0
}

// RecoveryClusterName builds a DNS-1123 subdomain (≤63 chars) for a temporary
// recovery cluster. Tenant names may contain underscores (per pg.IdentSafe);
// K8s metadata.name forbids them, so we normalize before composing.
func RecoveryClusterName(app string) string {
	const prefix = "pg-restore-"
	suffix := fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff)
	maxLen := 63 - len(prefix) - len(suffix)
	if maxLen < 1 {
		maxLen = 1
	}
	base := dns1123Base(app)
	if len(base) > maxLen {
		base = base[:maxLen]
	}
	s := prefix + base + suffix
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.TrimRight(s, "-")
}

// dns1123Base lowercases, replaces underscores with hyphens, drops other
// non-alnum-hyphen runes, and collapses repeated hyphens. Mirrors the
// helper in tenant.K8sSecretName / workspace.SecretPath.
func dns1123Base(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(strings.ToLower(s)), "_", "-")
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
