package k8s

import (
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ClusterClients struct {
	RESTConfig     *rest.Config
	Clientset      kubernetes.Interface
	Dynamic        dynamic.Interface
	Discovery      discovery.DiscoveryInterface
}

func NewClusterClients(kubeconfigPath string) (*ClusterClients, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("build config from %s: %w", kubeconfigPath, err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}

	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}

	return &ClusterClients{
		RESTConfig: cfg,
		Clientset:  clientset,
		Dynamic:    dyn,
		Discovery:  disc,
	}, nil
}
