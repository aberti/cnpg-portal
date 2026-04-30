package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRootCommandHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("help exec: %v", err)
	}

	got := out.String()
	for _, want := range []string{"new", "list", "status", "psql", "branch", "backup", "dump", "drop", "serve", "version"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing subcommand %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("version exec: %v", err)
	}

	if !strings.HasPrefix(out.String(), "cnpgctl ") {
		t.Errorf("unexpected version output: %q", out.String())
	}
}
