// Package k8s wraps the bits of client-go that cnpg-portal actually uses:
// loading a kubeconfig (or in-cluster config), holding a Clientset, and
// running remote-exec calls against the CNPG pod.
//
// Higher-level packages depend only on the small surface defined here so the
// rest of the codebase stays free of client-go imports.
package k8s

import (
	"fmt"
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is a thin wrapper around a typed Clientset, a dynamic client (for
// CRDs like CNPG's Backup), and the REST config used to build them. REST
// config drives remote-exec SPDY; Clientset handles typed Get/Create/Update;
// Dynamic handles the CRDs we don't generate typed bindings for.
type Client struct {
	Clientset  kubernetes.Interface
	Dynamic    dynamic.Interface
	RestConfig *rest.Config
}

// NewClient resolves a Kubernetes connection in this order:
//  1. In-cluster (when $KUBERNETES_SERVICE_HOST is present and a token is
//     mounted) — used by the cnpgctl serve pod.
//  2. The kubeconfig path argument (when non-empty).
//  3. The default loader chain ($KUBECONFIG, then ~/.kube/config).
func NewClient(kubeconfig string) (*Client, error) {
	cfg, err := loadConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	return &Client{Clientset: cs, Dynamic: dyn, RestConfig: cfg}, nil
}

func loadConfig(kubeconfig string) (*rest.Config, error) {
	if _, inCluster := os.LookupEnv("KUBERNETES_SERVICE_HOST"); inCluster {
		if cfg, err := rest.InClusterConfig(); err == nil {
			return cfg, nil
		}
		// Fall through to file-based loading if in-cluster discovery fails;
		// useful for local kind clusters where the env var leaks but no
		// service account token is mounted.
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}
