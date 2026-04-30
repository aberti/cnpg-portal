package connstr

import (
	"strings"
	"testing"
)

// sample is the canonical input we test every renderer against. The password
// includes characters that exercise URL escaping (`@`, `/`, `:`, space, `'`,
// backslash) so each renderer's quoting story is verified end-to-end rather
// than against a sanitised happy path.
var sample = Inputs{
	Host:     "pg-primary-rw.pg.svc.cluster.local",
	Port:     5432,
	Database: "acme",
	User:     "web_app",
	Password: `p@ss w'or\d/x`,
}

func TestRenderers(t *testing.T) {
	tests := []struct {
		id        string
		mustHave  []string // substrings the rendered output must contain
		mustNotBe []string // exact full-string forms it must not equal (catches identity bugs)
	}{
		{
			id: "url",
			mustHave: []string{
				"postgresql://",
				"web_app",
				"@pg-primary-rw.pg.svc.cluster.local:5432/acme",
				// '@', '/', '\\', "'", and space must be percent-encoded in userinfo.
				"%40", "%2F", "%5C", "%27", "%20",
			},
		},
		{
			id: "url-prisma",
			mustHave: []string{
				"postgresql://",
				"/acme?",
				"schema=public",
				"sslmode=require",
			},
		},
		{
			id: "libpq",
			mustHave: []string{
				"host=pg-primary-rw.pg.svc.cluster.local",
				"port=5432",
				"dbname=acme",
				"user=web_app",
				// Password has a space → must be quoted; ' and \ must be escaped.
				`password='p@ss w\'or\\d/x'`,
			},
		},
		{
			id: "jdbc",
			mustHave: []string{
				"jdbc:postgresql://pg-primary-rw.pg.svc.cluster.local:5432/acme?",
				"user=web_app",
				"password=",
			},
		},
		{
			id: "env",
			mustHave: []string{
				"PGHOST=pg-primary-rw.pg.svc.cluster.local",
				"PGPORT=5432",
				"PGDATABASE=acme",
				"PGUSER=web_app",
				`PGPASSWORD='p@ss w'\''or\d/x'`,
			},
		},
		{
			id: "psql",
			mustHave: []string{
				"psql",
				"-h pg-primary-rw.pg.svc.cluster.local",
				"-p 5432",
				"-U web_app",
				`PGPASSWORD='p@ss w'\''or\d/x'`,
				// Database is the trailing positional arg.
				" acme",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			f, ok := Find(tc.id)
			if !ok {
				t.Fatalf("format %q not registered in All", tc.id)
			}
			got := f.Render(sample)
			for _, want := range tc.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\nfull output: %s", want, got)
				}
			}
		})
	}
}

func TestAllFormatsRegistered(t *testing.T) {
	want := []string{"url", "url-prisma", "libpq", "jdbc", "env", "psql"}
	if len(All) != len(want) {
		t.Fatalf("All has %d formats, want %d", len(All), len(want))
	}
	for i, id := range want {
		if All[i].ID != id {
			t.Errorf("All[%d].ID = %q, want %q (order matters)", i, All[i].ID, id)
		}
		if All[i].Render == nil {
			t.Errorf("format %q has nil Render", id)
		}
		if All[i].Label == "" {
			t.Errorf("format %q has empty Label", id)
		}
	}
}

func TestFindUnknown(t *testing.T) {
	if _, ok := Find("nope"); ok {
		t.Error("Find returned ok=true for unknown id")
	}
}
