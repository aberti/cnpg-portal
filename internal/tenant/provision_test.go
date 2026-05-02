package tenant

import (
	"strings"
	"testing"
)

func TestParseSecretPassword(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		err  bool
	}{
		{
			name: "stringData single-quoted",
			body: "apiVersion: v1\nstringData:\n  password: 'abc123'\n",
			want: "abc123",
		},
		{
			name: "stringData unquoted",
			body: "stringData:\n  password: abc123\n",
			want: "abc123",
		},
		{
			name: "data base64-decoded form (after sops decrypt the field is plain)",
			body: "data:\n  password: dGVzdA==\n",
			want: "dGVzdA==",
		},
		{
			name: "missing field",
			body: "kind: Secret\n",
			err:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSecretPassword([]byte(tc.body))
			if tc.err {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSecretYAML(t *testing.T) {
	out := string(buildSecretYAML("acme-pg-credentials", "pg", "acme", "abc'def"))
	if !strings.Contains(out, "name: acme-pg-credentials") {
		t.Errorf("missing name: %s", out)
	}
	if !strings.Contains(out, "namespace: pg") {
		t.Errorf("missing namespace: %s", out)
	}
	// CNPG ≥ 1.25 rejects passwordSecrets that aren't basic-auth with both keys.
	if !strings.Contains(out, "type: kubernetes.io/basic-auth") {
		t.Errorf("secret must be kubernetes.io/basic-auth: %s", out)
	}
	if !strings.Contains(out, "username: 'acme'") {
		t.Errorf("missing username key: %s", out)
	}
	// Embedded single quote must be doubled inside the single-quoted scalar.
	if !strings.Contains(out, "password: 'abc''def'") {
		t.Errorf("password not escaped correctly: %s", out)
	}
}

func TestGeneratePasswordEntropyAndShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if len(pw) < 24 {
			t.Errorf("password too short (%d): %q", len(pw), pw)
		}
		if seen[pw] {
			t.Errorf("collision after %d iterations: %q", i, pw)
		}
		seen[pw] = true
	}
}

func TestBuildDatabaseURL(t *testing.T) {
	got := buildDatabaseURL("pg-primary", "acme", "p@ss/word", "pg", "acme")
	want := "postgresql://acme:p%40ss%2Fword@pg-primary-rw.pg.svc.cluster.local:5432/acme"
	if got != want {
		t.Errorf("buildDatabaseURL =\n  %s\nwant\n  %s", got, want)
	}
}
