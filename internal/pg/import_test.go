package pg

import (
	"bytes"
	"io"
	"testing"
)

func TestDetectDumpFormat(t *testing.T) {
	t.Parallel()
	customHdr := []byte("PGDMP\x01\x02\x03\x04")
	format, r, err := DetectDumpFormat(bytes.NewReader(customHdr))
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	if format != "custom" {
		t.Fatalf("format = %q, want custom", format)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(customHdr) {
		t.Fatalf("reader lost bytes")
	}

	sql := []byte("CREATE TABLE")
	format, r, err = DetectDumpFormat(bytes.NewReader(sql))
	if err != nil {
		t.Fatalf("sql: %v", err)
	}
	if format != "sql" {
		t.Fatalf("format = %q, want sql", format)
	}
	out, err = io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(sql) {
		t.Fatalf("sql reader lost bytes")
	}
}
