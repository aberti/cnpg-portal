package cnpg

import (
	"strings"
	"testing"
)

func TestBackupObjectNameOK(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"ondemand-ui-123", true},
		{"PG-primary_backup", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := BackupObjectNameOK(tt.name); got != tt.want {
			t.Errorf("BackupObjectNameOK(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRecoveryClusterNameLength(t *testing.T) {
	s := RecoveryClusterName("a")
	if len(s) > 63 {
		t.Fatalf("len=%d: %q", len(s), s)
	}
	long := strings.Repeat("x", 60)
	s2 := RecoveryClusterName(long)
	if len(s2) > 63 {
		t.Fatalf("len=%d", len(s2))
	}
}

func TestRecoveryClusterNameNormalizesUnderscore(t *testing.T) {
	s := RecoveryClusterName("app_dev")
	if strings.Contains(s, "_") {
		t.Errorf("RecoveryClusterName(app_dev)=%q still contains _", s)
	}
	if !strings.HasPrefix(s, "pg-restore-app-dev-") {
		t.Errorf("RecoveryClusterName(app_dev)=%q want prefix pg-restore-app-dev-", s)
	}
}
