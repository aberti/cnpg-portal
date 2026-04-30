// Package clusteryaml edits the CNPG Cluster manifest's spec.managed.roles
// sequence in place. It is the single chokepoint for cluster.yaml mutations
// performed by cnpg-portal, used by both the CLI (Provision/Drop) and the
// PR-bot. Comments at the document and field level are preserved; only the
// roles sequence is rewritten.
package clusteryaml

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ErrRoleExists signals that AppendRole found a role with the same name
// already declared in the manifest. The file is left unchanged.
var ErrRoleExists = errors.New("role already exists")

// Role is the subset of CNPG's RoleConfiguration that cnpg-portal manages
// declaratively. Fields not set here keep CNPG's defaults.
type Role struct {
	Name           string
	Comment        string
	PasswordSecret string
}

// AppendRole appends a tenant role to spec.managed.roles. Idempotent: when
// a role with the same Name already exists, returns ErrRoleExists with the
// file unchanged. Caller can ignore that error to treat re-runs as no-ops.
func AppendRole(path string, role Role) error {
	if role.Name == "" || role.PasswordSecret == "" {
		return errors.New("role.Name and role.PasswordSecret are required")
	}
	return modify(path, func(roles *yaml.Node) error {
		for _, n := range roles.Content {
			if scalarValue(findChild(n, "name")) == role.Name {
				return ErrRoleExists
			}
		}
		roles.Content = append(roles.Content, roleNode(role))
		return nil
	})
}

// RemoveRole removes a role by name. Idempotent: missing role leaves the
// file unchanged and returns nil.
func RemoveRole(path, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return modify(path, func(roles *yaml.Node) error {
		kept := roles.Content[:0]
		for _, n := range roles.Content {
			if scalarValue(findChild(n, "name")) == name {
				continue
			}
			kept = append(kept, n)
		}
		roles.Content = kept
		return nil
	})
}

// ListRoles returns the set of roles currently declared in cluster.yaml in
// declaration order.
func ListRoles(path string) ([]Role, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	roles, err := navigateRoles(&doc)
	if err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(roles.Content))
	for _, n := range roles.Content {
		out = append(out, Role{
			Name:           scalarValue(findChild(n, "name")),
			Comment:        scalarValue(findChild(n, "comment")),
			PasswordSecret: scalarValue(findChild(findChild(n, "passwordSecret"), "name")),
		})
	}
	return out, nil
}

func modify(path string, fn func(*yaml.Node) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	roles, err := navigateRoles(&doc)
	if err != nil {
		return err
	}
	if err := fn(roles); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func navigateRoles(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, errors.New("not a yaml document")
	}
	root := doc.Content[0]
	spec := findChild(root, "spec")
	if spec == nil {
		return nil, errors.New("missing spec")
	}
	managed := findChild(spec, "managed")
	if managed == nil {
		return nil, errors.New("missing spec.managed (no declarative role management configured)")
	}
	roles := findChild(managed, "roles")
	if roles == nil || roles.Kind != yaml.SequenceNode {
		return nil, errors.New("missing or invalid spec.managed.roles sequence")
	}
	return roles, nil
}

func findChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalarValue(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// roleNode constructs the role mapping with the project's standard
// ensure/login/createdb/inherit/connectionLimit defaults.
func roleNode(role Role) *yaml.Node {
	scalar := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }
	boolNode := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v} }
	intNode := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v} }
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			scalar("name"), scalar(role.Name),
			scalar("comment"), scalar(role.Comment),
			scalar("ensure"), scalar("present"),
			scalar("login"), boolNode("true"),
			scalar("createdb"), boolNode("false"),
			scalar("inherit"), boolNode("true"),
			scalar("connectionLimit"), intNode("-1"),
			scalar("passwordSecret"), {
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					scalar("name"), scalar(role.PasswordSecret),
				},
			},
		},
	}
}
