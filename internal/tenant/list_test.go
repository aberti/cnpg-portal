package tenant

import "testing"

func TestParseListRow(t *testing.T) {
	cases := []struct {
		name string
		row  []string
		want Tenant
		ok   bool
	}{
		{
			name: "happy",
			row:  []string{"acme", "acme", "8589934592", "3"},
			want: Tenant{Name: "acme", Role: "acme", Database: "acme", SecretName: "acme-pg-credentials", Owner: "acme", SizeBytes: 8589934592, Connections: 3},
			ok:   true,
		},
		{
			name: "trims whitespace",
			row:  []string{"acme ", "  postgres", "0", " 0"},
			want: Tenant{Name: "acme", Role: "acme", Database: "acme", SecretName: "acme-pg-credentials", Owner: "postgres", SizeBytes: 0, Connections: 0},
			ok:   true,
		},
		{
			name: "missing columns",
			row:  []string{"acme", "acme"},
			ok:   false,
		},
		{
			name: "empty name",
			row:  []string{"", "x", "0", "0"},
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseListRow(tc.row)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{2 * 1024 * 1024, "2.0 MiB"},
		{1024 * 1024 * 1024, "1.00 GiB"},
		{15 * 1024 * 1024 * 1024, "15.00 GiB"},
	}
	for _, tc := range cases {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
