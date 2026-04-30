package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeMarker(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, MarkerName), []byte(body), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

// chdir cd's to dir for the duration of the test, restoring the original
// working directory in t.Cleanup. (testing.T.Chdir is Go 1.24+.)
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestFindOverrideAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	// Marker provides only the deployment-specific values; the universal
	// CNPG conventions (namespace=pg, container=postgres, secrets/pg) are
	// applied as defaults.
	writeMarker(t, dir, "cluster_yaml: cluster.yaml\npod: cnpg-1\n")
	w, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if w.Root != dir {
		t.Errorf("Root = %q, want %q", w.Root, dir)
	}
	if w.ClusterYAML != "cluster.yaml" || w.Pod != "cnpg-1" {
		t.Errorf("marker fields not loaded: cluster_yaml=%q pod=%q", w.ClusterYAML, w.Pod)
	}
	if w.Namespace != "pg" || w.Container != "postgres" || w.SecretsDir != "secrets/pg" {
		t.Errorf("defaults wrong: ns=%s container=%s secrets=%s", w.Namespace, w.Container, w.SecretsDir)
	}
}

func TestFindOverrideHonorsCustomMarker(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "namespace: custom\npod: custom-pod\n")
	w, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if w.Namespace != "custom" || w.Pod != "custom-pod" {
		t.Errorf("custom marker not applied: ns=%s pod=%s", w.Namespace, w.Pod)
	}
	if w.Container != "postgres" {
		t.Errorf("Container default not applied alongside custom: %s", w.Container)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMarker(t, root, "")
	chdir(t, deep)
	w, err := Find("")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	wantRoot, _ := filepath.Abs(root)
	if w.Root != wantRoot {
		t.Errorf("Root = %q, want %q", w.Root, wantRoot)
	}
}

func TestFindNotFound(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv(EnvVar, "")
	_, err := Find("")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSecretPathAndClusterYAMLPath(t *testing.T) {
	w := Workspace{Root: "/tmp/root", ClusterYAML: "cluster.yaml"}
	w.applyDefaults()
	if got := w.ClusterYAMLPath(); got != "/tmp/root/cluster.yaml" {
		t.Errorf("ClusterYAMLPath = %q", got)
	}
	if got := w.SecretPath("acme"); got != "/tmp/root/secrets/pg/acme-pg-credentials.sops.yaml" {
		t.Errorf("SecretPath = %q", got)
	}
	// Underscores in app names must normalize to hyphens so the filename
	// matches the in-cluster Secret metadata.name.
	if got := w.SecretPath("web_user"); got != "/tmp/root/secrets/pg/web-user-pg-credentials.sops.yaml" {
		t.Errorf("SecretPath(web_user) = %q", got)
	}
}
