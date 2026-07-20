package main

import (
	"github.com/aberti/cnpg-portal/internal/k8s"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/web"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

// buildDeps wires the standard Deps bundle every CLI verb uses. Pulled out
// so each subcommand stays focused on its specific arg parsing + output.
func buildDeps(workspaceFlag, kubeconfigFlag string) (tenant.Deps, error) {
	ws, err := workspace.FindCluster(workspaceFlag, clusterFlag)
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

func buildClusterRegistry(workspaceFlag, kubeconfigFlag string) (web.ClusterRegistry, error) {
	catalog, err := workspace.FindCatalog(workspaceFlag)
	if err != nil {
		return web.ClusterRegistry{}, err
	}
	kube, err := k8s.NewClient(kubeconfigFlag)
	if err != nil {
		return web.ClusterRegistry{}, err
	}
	targets := make([]web.ClusterTarget, 0, len(catalog.Clusters))
	for _, id := range catalog.IDs() {
		ws, err := catalog.Select(id)
		if err != nil {
			return web.ClusterRegistry{}, err
		}
		targets = append(targets, web.ClusterTarget{
			ID:          id,
			DisplayName: ws.DisplayName,
			Deps: tenant.Deps{
				K8s: kube,
				PG: &pg.Conn{
					K8s:       kube,
					Namespace: ws.Namespace,
					Pod:       ws.Pod,
					Container: ws.Container,
				},
				Workspace: ws,
			},
		})
	}
	return web.NewClusterRegistry(catalog.DefaultCluster, targets)
}
