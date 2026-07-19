package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/aberti/cnpg-portal/internal/connstr"
	"github.com/aberti/cnpg-portal/internal/tenant"
)

func TestTenantDetailDegradedWhenCredsNil(t *testing.T) {
	tn := &tenant.Tenant{Name: "acme", Database: "acme", SecretName: "acme-pg-credentials"}
	// Admin sees the connection-strings panel (with degraded notice when
	// creds is nil); viewer never sees it at all.
	var b strings.Builder
	if err := TenantDetail(tn, nil, true).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, "Connection strings") {
		t.Error("expected 'Connection strings' header in output (admin)")
	}
	if !strings.Contains(out, "Credentials Secret not readable") {
		t.Error("expected degraded-mode notice when creds is nil (admin)")
	}
	for _, f := range connstr.All {
		if strings.Contains(out, ">"+f.Label+"<") {
			t.Errorf("format %q rendered despite nil creds", f.Label)
		}
	}
}

func TestTenantDetailHidesConnStringsFromViewer(t *testing.T) {
	tn := &tenant.Tenant{Name: "acme", Database: "acme", SecretName: "acme-pg-credentials"}
	creds := &tenant.Credentials{
		Host: "pg-primary-rw.pg.svc.cluster.local", Port: 5432,
		Database: "acme", User: "acme", Password: "secret123",
	}
	var b strings.Builder
	if err := TenantDetail(tn, creds, false).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "Connection strings") {
		t.Error("viewer should not see the Connection strings panel")
	}
	if strings.Contains(out, "secret123") {
		t.Error("viewer page leaked the password")
	}
}

func TestTenantDetailRendersAllFormats(t *testing.T) {
	tn := &tenant.Tenant{Name: "acme", Database: "acme", SecretName: "acme-pg-credentials"}
	creds := &tenant.Credentials{
		Host:     "pg-primary-rw.pg.svc.cluster.local",
		Port:     5432,
		Database: "acme",
		User:     "acme",
		Password: "secret123",
	}
	var b strings.Builder
	if err := TenantDetail(tn, creds, true).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	for _, f := range connstr.All {
		if !strings.Contains(out, f.Label) {
			t.Errorf("expected format label %q in output", f.Label)
		}
	}
	// The URL form should be there end-to-end.
	if !strings.Contains(out, "postgresql://acme:secret123@pg-primary-rw.pg.svc.cluster.local:5432/acme") {
		t.Error("expected URL form in output")
	}
	// Copy button should be present once per format.
	if got := strings.Count(out, ">Copy</button>"); got != len(connstr.All) {
		t.Errorf("expected %d copy buttons, got %d", len(connstr.All), got)
	}
	// Password must NOT leak as plaintext into a URL via wrong escaping —
	// for this benign password the renderer leaves it unescaped, which is
	// correct. Smoke-test just confirms no double-encoding.
	if strings.Contains(out, "secret123secret123") {
		t.Error("password appears doubled — likely a render bug")
	}
}

func TestTenantDetailKeepsClusterScopeAndHidesSharedOwnerCredentials(t *testing.T) {
	tn := &tenant.Tenant{
		Name: "legacy_reporting", Database: "legacy_reporting", Owner: "postgres",
		SecretName: "legacy-reporting-pg-credentials",
	}
	ctx := WithPageContext(context.Background(), PageContext{
		BasePath:    "/clusters/analytics",
		ClusterID:   "analytics",
		ClusterName: "postgres-analytics",
		DisplayName: "Analytics",
		Namespace:   "pg",
	})
	var b strings.Builder
	if err := TenantDetail(tn, nil, true).Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, `href="/clusters/analytics/"`) {
		t.Error("tenant detail lost the active cluster scope")
	}
	if strings.Contains(out, "/tenant/legacy_reporting/rotate") {
		t.Error("shared-owner database exposed password rotation")
	}
	if !strings.Contains(out, "Shared or unmanaged owner") {
		t.Error("shared-owner database lacks an explicit unmanaged badge")
	}
	if !strings.Contains(out, "cnpgctl --cluster analytics") {
		t.Error("CLI snippets do not identify the active cluster")
	}
}
