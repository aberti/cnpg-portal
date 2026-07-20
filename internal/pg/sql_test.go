package pg

import "testing"

func TestIdentSafe(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"acme", true},
		{"web_app", true},
		{"a", true},
		{"a1", true},
		{"a_b_c_2", true},
		{"", false},
		{"1abc", false}, // can't start with digit
		{"Abc", false},  // uppercase rejected
		{"a-b", false},  // hyphen rejected
		{"a;DROP TABLE x", false},
		{"\"quoted\"", false},
		{string(make([]byte, 64)), false}, // too long
	}
	for _, tc := range cases {
		if got := IdentSafe(tc.in); got != tc.want {
			t.Errorf("IdentSafe(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDatabaseNameSafe(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"billing_api", true},
		{"stg-example1", true},
		{"a-b_c2", true},
		{"", false},
		{"1database", false},
		{"UPPER", false},
		{"a/b", false},
		{"a;DROP DATABASE postgres", false},
		{string(make([]byte, 64)), false},
	}
	for _, tc := range cases {
		if got := DatabaseNameSafe(tc.in); got != tc.want {
			t.Errorf("DatabaseNameSafe(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	q, err := QuoteIdent("acme")
	if err != nil || q != `"acme"` {
		t.Errorf("QuoteIdent(evs) = %q, %v", q, err)
	}
	if _, err := QuoteIdent("a; DROP"); err == nil {
		t.Errorf("QuoteIdent(unsafe): err = nil, want rejection")
	}
}

func TestLiteralEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{"O'Brien", "O''Brien"},
		{"''", "''''"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := LiteralEscape(tc.in); got != tc.want {
			t.Errorf("LiteralEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
