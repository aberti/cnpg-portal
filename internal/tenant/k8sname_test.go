package tenant

import "testing"

func TestK8sSecretName(t *testing.T) {
	cases := []struct {
		app  string
		want string
	}{
		{"acme", "acme-pg-credentials"},
		{"aber1_dev", "aber1-dev-pg-credentials"},
		{"a_b", "a-b-pg-credentials"},
	}
	for _, tc := range cases {
		if got := K8sSecretName(tc.app); got != tc.want {
			t.Errorf("K8sSecretName(%q) = %q, want %q", tc.app, got, tc.want)
		}
	}
}
