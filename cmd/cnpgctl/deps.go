package main

import (
	"github.com/aberti/cnpg-portal/internal/k8s"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

// buildDeps wires the standard Deps bundle every CLI verb uses. Pulled out
// so each subcommand stays focused on its specific arg parsing + output.
func buildDeps(workspaceFlag, kubeconfigFlag string) (tenant.Deps, error) {
	ws, err := workspace.Find(workspaceFlag)
	if err != nil {
		return tenant.Deps{}, err
	}
	kube, err := k8s.NewClient(kubeconfigFlag)
	if err != nil {
		return tenant.Deps{}, err
	}
	return tenant.Deps{
		K8s: kube,
		PG: &pg.Conn{
			K8s:       kube,
			Namespace: ws.Namespace,
			Pod:       ws.Pod,
			Container: ws.Container,
		},
		Workspace: ws,
	}, nil
}
