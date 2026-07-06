package cnpg

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseBackup(t *testing.T) {
	item := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "pg-primary-2026-04-27-02"},
		"spec":     map[string]any{"cluster": map[string]any{"name": "pg-primary"}},
		"status": map[string]any{
			"backupId":        "20260706T125909",
			"method":          "plugin",
			"phase":           "completed",
			"startedAt":       "2026-04-27T02:00:00Z",
			"stoppedAt":       "2026-04-27T02:01:30Z",
			"destinationPath": "s3://ot-cnpg-backups/pg-primary",
		},
	}}
	b := parseBackup(item)
	if b.Name != "pg-primary-2026-04-27-02" || b.Cluster != "pg-primary" || b.Phase != "completed" {
		t.Errorf("unexpected backup: %+v", b)
	}
	if b.BackupID != "20260706T125909" || b.Method != "plugin" {
		t.Errorf("unexpected plugin metadata: %+v", b)
	}
	wantStart, _ := time.Parse(time.RFC3339, "2026-04-27T02:00:00Z")
	if !b.StartedAt.Equal(wantStart) {
		t.Errorf("StartedAt = %v, want %v", b.StartedAt, wantStart)
	}
	if b.DestPath != "s3://ot-cnpg-backups/pg-primary" {
		t.Errorf("DestPath = %q", b.DestPath)
	}
}

func TestParseBackupTolerantOfMissingFields(t *testing.T) {
	item := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "incomplete"},
	}}
	b := parseBackup(item)
	if b.Name != "incomplete" || b.Cluster != "" || b.Phase != "" {
		t.Errorf("unexpected backup: %+v", b)
	}
	if !b.StartedAt.IsZero() || !b.StoppedAt.IsZero() {
		t.Errorf("times should be zero: %v %v", b.StartedAt, b.StoppedAt)
	}
}

func TestLatestCompleted(t *testing.T) {
	mk := func(name, phase, started string) Backup {
		t, _ := time.Parse(time.RFC3339, started)
		return Backup{Name: name, Phase: phase, StartedAt: t}
	}
	// Sorted newest first as ListBackups returns.
	backups := []Backup{
		mk("c", "running", "2026-04-27T03:00:00Z"),
		mk("b", "completed", "2026-04-27T02:00:00Z"),
		mk("a", "completed", "2026-04-26T02:00:00Z"),
	}
	got := LatestCompleted(backups)
	if got.Name != "b" {
		t.Errorf("got %s, want b", got.Name)
	}

	if (LatestCompleted([]Backup{mk("x", "failed", "2026-04-27T02:00:00Z")}) != Backup{}) {
		t.Errorf("expected zero Backup for no completed entries")
	}
	if (LatestCompleted(nil) != Backup{}) {
		t.Errorf("expected zero Backup for nil input")
	}
}
