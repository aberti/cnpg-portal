package tenant

import (
	"strings"
)

// K8sSecretName returns the in-cluster Secret metadata.name for an app's
// postgres credentials (suffix "-pg-credentials").
//
// App identifiers follow pg.IdentSafe (may contain '_'). Kubernetes object
// names must be RFC 1123 subdomains, so underscores become hyphens and the
// result is trimmed to valid label boundaries.
func K8sSecretName(app string) string {
	return dns1123Label(app) + "-pg-credentials"
}

func dns1123Label(app string) string {
	s := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(app)), "_", "-")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	s = b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "app"
	}
	return s
}
