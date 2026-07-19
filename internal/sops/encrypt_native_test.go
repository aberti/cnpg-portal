package sops

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// TestEncryptYAMLForRecipient generates a fresh age keypair, encrypts a
// sample Secret YAML, and asserts the output (a) contains the SOPS
// metadata block, (b) no longer contains the plaintext password value,
// (c) preserves the document's structural keys.
//
// We intentionally don't test round-trip via decryption here — that
// would require either pulling in the sops decryption surface or an
// external `sops` binary. A round-trip smoke test is in the integration
// suite (operator runs `sops -d` on the produced file).
func TestEncryptYAMLForRecipient(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	recipient := identity.Recipient().String()
	if !strings.HasPrefix(recipient, "age1") {
		t.Fatalf("recipient looks malformed: %q", recipient)
	}

	plain := []byte(`apiVersion: v1
kind: Secret
metadata:
  name: acme-pg-credentials
  namespace: pg
type: Opaque
stringData:
  password: super-secret-pw-do-not-leak
`)

	encrypted, err := EncryptYAMLForRecipient(plain, recipient)
	if err != nil {
		t.Fatalf("EncryptYAMLForRecipient: %v", err)
	}

	got := string(encrypted)

	// (a) sops metadata appended.
	if !strings.Contains(got, "sops:") {
		t.Errorf("output missing sops metadata block:\n%s", got)
	}
	if !strings.Contains(got, "age:") {
		t.Errorf("output missing age key entry:\n%s", got)
	}

	// (b) no plaintext password leaking through.
	if bytes.Contains(encrypted, []byte("super-secret-pw-do-not-leak")) {
		t.Errorf("plaintext password found in encrypted output (encryption broken)")
	}

	// (c) Structural keys preserved. SOPS default behavior encrypts every
	// leaf value (so `kind: Secret` becomes `kind: ENC[…]`); only the
	// keys themselves stay readable. We check for the trailing colon to
	// catch the unencrypted key while tolerating the encrypted value.
	for _, want := range []string{"apiVersion:", "kind:", "metadata:", "name:", "namespace:", "type:", "stringData:", "password:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing structural key %q", want)
		}
	}
	// And the encrypted-value sentinel from sops.
	if !strings.Contains(got, "ENC[AES256_GCM,") {
		t.Errorf("output missing AES256_GCM ciphertext markers — values weren't encrypted?\n%s", got)
	}
}

func TestEncryptYAMLForRecipientRejectsBadRecipient(t *testing.T) {
	_, err := EncryptYAMLForRecipient([]byte("kind: Foo\n"), "not-an-age-key")
	if err == nil {
		t.Errorf("expected error for malformed recipient, got nil")
	}
}

func TestEncryptForWorkspaceWritesOnlyCiphertext(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	config := "creation_rules:\n  - path_regex: secrets/.*\\.sops\\.yaml$\n    age: " +
		identity.Recipient().String() + "\n"
	if err := os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "secrets", "pg", "acme.sops.yaml")
	plain := []byte("stringData:\n  password: never-write-this-plaintext\n")

	encrypted, err := EncryptForWorkspace(root, target, plain)
	if err != nil {
		t.Fatalf("EncryptForWorkspace: %v", err)
	}
	if err := WriteEncryptedAtomic(target, encrypted); err != nil {
		t.Fatalf("WriteEncryptedAtomic: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("never-write-this-plaintext")) {
		t.Fatal("workspace file contains plaintext")
	}
	if !bytes.Contains(got, []byte("sops:")) {
		t.Fatal("workspace file does not contain SOPS metadata")
	}
}
