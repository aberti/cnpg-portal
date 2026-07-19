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

func TestSecurityHeadersAndCrossOriginMutationGuard(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/new")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Security-Policy"); got == "" {
		t.Error("missing Content-Security-Policy")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST status = %d, want 403", res.StatusCode)
	}
}

func TestConfiguredPublicOriginPassesMutationGuard(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	admins := &AdminList{logins: map[string]struct{}{"test@example.com": {}}}
	h := Router(tenant.Deps{}, logger, Auth{
		DevLogin:       "test@example.com",
		Admins:         admins,
		AllowedOrigins: []string{"https://portal.example"},
	})

	for _, origin := range []string{
		"https://portal.example",
		"https://portal.example/",
		"https://PORTAL.EXAMPLE:443",
	} {
		req := httptest.NewRequest(http.MethodPost, "http://internal.example/backup", nil)
		req.Header.Set("Origin", origin)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)

		if res.Code != http.StatusSeeOther {
			t.Errorf("allowed public-origin POST from %q status = %d, want 303", origin, res.Code)
		}
	}
}

func TestSameOriginFetchMetadataFallback(t *testing.T) {
	h := newAdminTestRouter()

	for _, origin := range []string{"", "null"} {
		req := httptest.NewRequest(http.MethodPost, "http://internal.example/backup", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)

		if res.Code != http.StatusSeeOther {
			t.Errorf("same-origin Fetch Metadata with Origin %q status = %d, want 303", origin, res.Code)
		}
	}
}

func TestFetchMetadataCannotOverrideForeignOrigin(t *testing.T) {
	h := newAdminTestRouter()
	req := httptest.NewRequest(http.MethodPost, "http://internal.example/backup", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Errorf("foreign Origin with same-origin Fetch Metadata status = %d, want 403", res.Code)
	}
}

func TestRouterClustersScopesKnownTargets(t *testing.T) {
	registry, err := NewClusterRegistry("platform", []ClusterTarget{
		{ID: "platform", DisplayName: "Shared platform"},
		{ID: "analytics", DisplayName: "Analytics"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := RouterClusters(registry, discardLogger(), Auth{DevLogin: "test@example.com"})

	root := httptest.NewRecorder()
	h.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusSeeOther || root.Header().Get("Location") != "/clusters/platform/" {
		t.Fatalf("root redirect = %d %q", root.Code, root.Header().Get("Location"))
	}

	known := httptest.NewRecorder()
	h.ServeHTTP(known, httptest.NewRequest(http.MethodGet, "/clusters/analytics/", nil))
	if known.Code != http.StatusServiceUnavailable {
		t.Fatalf("known target status = %d, want deps-degraded 503", known.Code)
	}

	unknown := httptest.NewRecorder()
	h.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/clusters/missing/", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown target status = %d, want 404", unknown.Code)
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

func TestImportDumpRequiresAdmin(t *testing.T) {
	srv := httptest.NewServer(newTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/new/import-dump")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestImportDumpFormWithAdminAndRemoteURLDisabled(t *testing.T) {
	srv := httptest.NewServer(newAdminTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/new/import-dump")
	if err != nil {
		t.Fatalf("get import dump: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("import dump status = %d, want 200", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/new/import-url")
	if err != nil {
		t.Fatalf("get import URL: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("remote URL import status = %d, want 404 because web SSRF surface is disabled", resp.StatusCode)
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
