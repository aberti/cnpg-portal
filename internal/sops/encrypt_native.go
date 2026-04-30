package sops

import (
	"fmt"

	getsops "github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	sopsCommon "github.com/getsops/sops/v3/cmd/sops/common"
	yamlstore "github.com/getsops/sops/v3/stores/yaml"
)

// EncryptYAMLForRecipient produces a SOPS-encrypted YAML blob equivalent
// to running `sops --encrypt --age <recipient>` against plain. It runs
// entirely in-process — no `sops` binary required — so the in-cluster
// cnpg-portal pod can emit ready-to-commit secret files for its PR-bot
// without bundling the SOPS CLI into the container image.
//
// The `sops` binary on the operator's laptop and this Go function emit
// byte-compatible output (round-trip tested): a file produced here can be
// `sops --decrypt`'d with the matching age private key, and a file
// produced by `sops --encrypt` can be parsed by `sops/v3` consumers
// downstream.
//
// recipient is the `age1...` public key for the operator's GitOps
// workspace — the same value that lives under `creation_rules[].age` in
// the workspace's `.sops.yaml`. plain is the YAML content the operator
// wants to encrypt — typically a Kubernetes Secret manifest with a
// single `stringData.password` field.
func EncryptYAMLForRecipient(plain []byte, recipient string) ([]byte, error) {
	store := &yamlstore.Store{}

	branches, err := store.LoadPlainFile(plain)
	if err != nil {
		return nil, fmt.Errorf("parse plain YAML: %w", err)
	}

	masterKey, err := age.MasterKeyFromRecipient(recipient)
	if err != nil {
		return nil, fmt.Errorf("invalid age recipient %q: %w", recipient, err)
	}

	tree := getsops.Tree{
		Branches: branches,
		Metadata: getsops.Metadata{
			KeyGroups: []getsops.KeyGroup{
				{masterKey},
			},
			Version: "3.9.2", // align with the sops binary version pinned in PG/.mise.toml
		},
	}

	dataKey, errs := tree.GenerateDataKey()
	if len(errs) > 0 {
		return nil, fmt.Errorf("generate data key: %w", errs[0])
	}

	if err := sopsCommon.EncryptTree(sopsCommon.EncryptTreeOpts{
		Cipher:  aes.NewCipher(),
		DataKey: dataKey,
		Tree:    &tree,
	}); err != nil {
		return nil, fmt.Errorf("encrypt tree: %w", err)
	}

	encrypted, err := store.EmitEncryptedFile(tree)
	if err != nil {
		return nil, fmt.Errorf("emit encrypted YAML: %w", err)
	}
	return encrypted, nil
}
