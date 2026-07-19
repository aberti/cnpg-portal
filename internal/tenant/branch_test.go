package tenant

import (
	"strings"
	"testing"

	"github.com/aberti/cnpg-portal/internal/pg"
)

func TestDefaultBranchDestination_typical(t *testing.T) {
	if got := DefaultBranchDestination("aber1"); got != "aber1_dev" {
		t.Errorf("got %q, want aber1_dev", got)
	}
}

func TestDefaultBranchDestination_externalDatabase(t *testing.T) {
	if got := DefaultBranchDestination("stg-example1"); got != "stg_example1_dev" {
		t.Errorf("got %q, want stg_example1_dev", got)
	}
}

func TestDefaultBranchDestination_longSourceFits63(t *testing.T) {
	// 59 letters + "_dev" = 63, the maximum IdentSafe length.
	var b strings.Builder
	b.WriteByte('a')
	for i := 0; i < 58; i++ {
		b.WriteByte('b')
	}
	src := b.String()
	if len(src) != 59 {
		t.Fatalf("src len = %d", len(src))
	}
	if !pg.IdentSafe(src) {
		t.Fatal("src should be IdentSafe")
	}
	got := DefaultBranchDestination(src)
	if len(got) != 63 {
		t.Fatalf("got len %d, want 63: %q", len(got), got)
	}
	if !pg.IdentSafe(got) {
		t.Fatalf("got not IdentSafe: %q", got)
	}
	if !strings.HasSuffix(got, "_dev") {
		t.Errorf("expected _dev suffix: %q", got)
	}
}
