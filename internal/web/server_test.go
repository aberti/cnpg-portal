package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func newTestRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// dev-login bypasses the tailnet header read so /, /tenant/* keep
	// hitting the deps-degraded paths these tests target. Mutation routes
	// are unaffected — Admins is nil so RequireAdmin still 403s.
	return Router(tenant.Deps{}, logger, Auth{DevLogin: "test@example.com"})
}

func newAdminTestRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	admins := &AdminList{logins: map[string]struct{}{"test@example.com": {}}}
	return Router(tenant.Deps{}, logger, Auth{DevLogin: "test@example.com", Admins: admins})
}

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-Id"); got == "" {
		t.Errorf("expected X-Request-Id header, got empty")
	}
}

func TestMetricsExposed(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestListTenantsWithoutDepsReturns503 exercises the graceful-degradation
// path: a server started without PG deps still responds with a usable
// HTML error page rather than panicking.
func TestListTenantsWithoutDepsReturns503(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestNewTenantRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/new")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestNewTenantFormWithAdmin(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/new")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestImportDumpAndURLRequireAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	for _, path := range []string{"/new/import-dump", "/new/import-url"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", path, resp.StatusCode)
		}
	}
}

func TestImportDumpAndURLFormWithAdmin(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	for _, path := range []string{"/new/import-dump", "/new/import-url"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestBranchTenantFormRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/branch")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestDropTenantFormRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/drop")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestBranchAndDropFormsWithAdmin(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	for _, path := range []string{"/tenant/acme/branch", "/tenant/acme/drop"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestRestoreRouteRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/restore")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRestoreFormAdminWithoutDepsIs503(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/restore")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no K8s deps in test router)", resp.StatusCode)
	}
}

func TestDevLoginImplicitAdminOpensMutationRoutes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	admins, err := NewAdminList("", logger)
	if err != nil {
		t.Fatalf("NewAdminList: %v", err)
	}
	admins.GrantDevFallback("test@example.com")
	srv := httptest.NewServer(Router(tenant.Deps{}, logger, Auth{DevLogin: "test@example.com", Admins: admins}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/branch")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("branch form status = %d, want 200 (implicit admin when no admin-list-path)", resp.StatusCode)
	}
}

func TestTenantConnectionsWithoutDepsReturns503(t *testing.T) {
	// /conns is admin-gated (renders running query text — equivalent to
	// data exfiltration). Non-admins get 403 before the deps check; use
	// the admin test router to exercise the deps-missing path.
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tenant/acme/conns")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestTerminateConnectionRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/tenant/acme/conns/123/terminate", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}
