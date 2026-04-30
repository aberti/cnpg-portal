package web

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aberti/cnpg-portal/internal/tenant"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIdentityMiddleware_RejectsMissingHeader(t *testing.T) {
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (no tailnet identity)", resp.StatusCode)
	}
}

func TestIdentityMiddleware_AcceptsHeader(t *testing.T) {
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set(HeaderLogin, "user@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	// Identity passes; the deps-empty path then returns 503.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (auth ok, deps missing)", resp.StatusCode)
	}
}

func TestIdentityMiddleware_DevLoginBypass(t *testing.T) {
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{DevLogin: "dev@local"}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (dev-login bypass + deps missing)", resp.StatusCode)
	}
}

func TestIdentityMiddleware_TailnetFallbackCGNAT(t *testing.T) {
	h := Router(tenant.Deps{}, discardLogger(), Auth{TailnetFallbackLogin: "op@example.com"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "100.100.1.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("status = 403, want identity ok (tailnet CGNAT)")
	}
}

func TestIdentityMiddleware_TailnetFallbackRejectsPublicIP(t *testing.T) {
	h := Router(tenant.Deps{}, discardLogger(), Auth{TailnetFallbackLogin: "op@example.com"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without tailnet IP", rec.Code)
	}
}

func TestIdentityMiddleware_BearerToken(t *testing.T) {
	admins, err := NewAdminList("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins.SetBearerAdmin("op@example.com")
	h := Router(tenant.Deps{}, discardLogger(), Auth{
		BearerToken:    "correct-horse-battery-staple",
		BearerIdentity: "op@example.com",
		Admins:         admins,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("status = 403, want identity via bearer")
	}
}

func TestIdentityMiddleware_BearerTokenWrongUnauthorized(t *testing.T) {
	admins, err := NewAdminList("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins.SetBearerAdmin("op@example.com")
	h := Router(tenant.Deps{}, discardLogger(), Auth{
		BearerToken:    "secret",
		BearerIdentity: "op@example.com",
		Admins:         admins,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for bad bearer", rec.Code)
	}
}

func TestAdminList_BearerAdminSurvivesFileReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admins")
	if err := os.WriteFile(path, []byte("a@x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := NewAdminList(path, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	a.SetBearerAdmin("bearer@x")
	if err := os.WriteFile(path, []byte("only@x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if !a.Has("bearer@x") {
		t.Fatal("bearer admin lost after reload")
	}
	if !a.Has("only@x") {
		t.Fatal("file admin missing")
	}
	if a.Has("a@x") {
		t.Fatal("old file admin should be gone")
	}
}

func TestIdentityMiddleware_TailnetFallbackUsesXForwardedFor(t *testing.T) {
	h := Router(tenant.Deps{}, discardLogger(), Auth{TailnetFallbackLogin: "op@example.com"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "100.101.2.3, 10.0.0.1")
	req.RemoteAddr = "10.42.0.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("expected first XFF hop to qualify as tailnet")
	}
}

func TestIsTailscaleCGNAT(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"100.64.0.1", true},
		{"100.100.1.1", true},
		{"100.127.255.255", true},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"10.0.0.1", false},
		{"127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := IsTailscaleCGNAT(net.ParseIP(tt.ip)); got != tt.want {
			t.Errorf("IsTailscaleCGNAT(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestRequireAdmin_BlocksNonAdmin(t *testing.T) {
	admins := writeAdminList(t, "admin@example.com\n")
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{Admins: admins}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/backup", nil)
	req.Header.Set(HeaderLogin, "user@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (non-admin)", resp.StatusCode)
	}
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	admins := writeAdminList(t, "admin@example.com\n")
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{Admins: admins}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/backup", nil)
	req.Header.Set(HeaderLogin, "admin@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	// Admin passes the gate; backup handler then degrades on missing K8s deps.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (admin allowed, deps-degraded HTML body)", resp.StatusCode)
	}
}

func TestRequireAdmin_NilAdminListFailsClosed(t *testing.T) {
	srv := httptest.NewServer(Router(tenant.Deps{}, discardLogger(), Auth{DevLogin: "anyone@example.com"}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/backup", "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (nil admin list = fail-closed)", resp.StatusCode)
	}
}

func TestAdminList_LoadAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admins")
	if err := os.WriteFile(path, []byte("a@x\n# comment\n\nb@x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a, err := NewAdminList(path, discardLogger())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !a.Has("a@x") || !a.Has("b@x") || a.Has("# comment") {
		t.Errorf("initial parse wrong: count=%d has(a)=%v has(b)=%v", a.Count(), a.Has("a@x"), a.Has("b@x"))
	}

	if err := os.WriteFile(path, []byte("c@x\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if a.Has("a@x") || !a.Has("c@x") {
		t.Errorf("after reload: a=%v c=%v", a.Has("a@x"), a.Has("c@x"))
	}
}

func TestAdminList_MissingFileIsEmptyNotError(t *testing.T) {
	a, err := NewAdminList(filepath.Join(t.TempDir(), "nope"), discardLogger())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if a.Count() != 0 {
		t.Errorf("count = %d, want 0 for missing file", a.Count())
	}
}

func TestAdminList_EmptyPath(t *testing.T) {
	a, err := NewAdminList("", discardLogger())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if a.Has("anyone") {
		t.Errorf("empty-path admin list should reject all logins")
	}
}

// writeAdminList writes a temp admin file containing the supplied content
// and returns a loaded AdminList rooted at it. Test scope.
func writeAdminList(t *testing.T, content string) *AdminList {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admins")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	a, err := NewAdminList(path, discardLogger())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !strings.Contains(content, "@") {
		t.Fatalf("test fixture should contain at least one email")
	}
	return a
}
