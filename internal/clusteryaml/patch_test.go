package clusteryaml

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("testdata/cluster.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "cluster.yaml")
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
	return dst
}

func TestListRoles(t *testing.T) {
	roles, err := ListRoles(setup(t))
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("len(roles) = %d, want 1", len(roles))
	}
	if roles[0].Name != "existing" {
		t.Errorf("name = %q, want existing", roles[0].Name)
	}
	if roles[0].PasswordSecret != "existing-pg-credentials" {
		t.Errorf("passwordSecret = %q", roles[0].PasswordSecret)
	}
}

func TestAppendRoleHappy(t *testing.T) {
	path := setup(t)
	err := AppendRole(path, Role{
		Name:           "newapp",
		Comment:        "newapp application user",
		PasswordSecret: "newapp-pg-credentials",
	})
	if err != nil {
		t.Fatalf("AppendRole: %v", err)
	}
	roles, err := ListRoles(path)
	if err != nil {
		t.Fatalf("ListRoles after append: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("len(roles) = %d, want 2", len(roles))
	}
	if roles[1].Name != "newapp" || roles[1].PasswordSecret != "newapp-pg-credentials" {
		t.Errorf("appended role wrong: %+v", roles[1])
	}
}

func TestAppendRoleIdempotent(t *testing.T) {
	path := setup(t)
	err := AppendRole(path, Role{Name: "existing", PasswordSecret: "existing-pg-credentials"})
	if !errors.Is(err, ErrRoleExists) {
		t.Errorf("AppendRole on existing: err = %v, want ErrRoleExists", err)
	}
	roles, _ := ListRoles(path)
	if len(roles) != 1 {
		t.Errorf("file mutated despite ErrRoleExists: %d roles", len(roles))
	}
}

func TestRemoveRoleHappy(t *testing.T) {
	path := setup(t)
	if err := RemoveRole(path, "existing"); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}
	roles, _ := ListRoles(path)
	if len(roles) != 0 {
		t.Errorf("len(roles) after remove = %d, want 0", len(roles))
	}
}

func TestRemoveRoleMissingIsNoOp(t *testing.T) {
	path := setup(t)
	if err := RemoveRole(path, "no-such-role"); err != nil {
		t.Errorf("RemoveRole(missing): %v, want nil", err)
	}
	roles, _ := ListRoles(path)
	if len(roles) != 1 {
		t.Errorf("file mutated by removing missing role: %d roles", len(roles))
	}
}

func TestAppendThenRemoveRoundTrip(t *testing.T) {
	path := setup(t)
	if err := AppendRole(path, Role{Name: "tmp", PasswordSecret: "tmp-pg-credentials"}); err != nil {
		t.Fatalf("AppendRole: %v", err)
	}
	if err := RemoveRole(path, "tmp"); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}
	roles, _ := ListRoles(path)
	if len(roles) != 1 || roles[0].Name != "existing" {
		t.Errorf("round-trip changed unrelated state: %+v", roles)
	}
}

func TestAppendRoleValidates(t *testing.T) {
	path := setup(t)
	if err := AppendRole(path, Role{Name: ""}); err == nil {
		t.Errorf("AppendRole(empty name): err = nil, want validation error")
	}
	if err := AppendRole(path, Role{Name: "x"}); err == nil {
		t.Errorf("AppendRole(no secret): err = nil, want validation error")
	}
}
