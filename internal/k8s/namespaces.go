package k8s

import (
	"context"
	"fmt"
	"sort"

	"github.com/jamie/drkate/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var defaultSystemNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

func ResolveNamespaces(ctx context.Context, cfg config.NamespaceConfig, clients *ClusterClients) ([]string, error) {
	hasInclude := len(cfg.Include) > 0
	hasExclude := len(cfg.Exclude) > 0
	hasAll := cfg.All

	var base []string

	switch {
	case hasInclude:
		base = append([]string{}, cfg.Include...)
	case hasAll || hasExclude:
		nsList, err := clients.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		for _, ns := range nsList.Items {
			base = append(base, ns.Name)
		}
	default:
		return nil, fmt.Errorf("invalid namespace config")
	}

	exclusions := make(map[string]bool)
	for _, e := range cfg.Exclude {
		exclusions[e] = true
	}

	if cfg.UseDefaultExclude(hasInclude) {
		for _, e := range defaultSystemNamespaces {
			exclusions[e] = true
		}
	}

	var result []string
	seen := make(map[string]bool)
	for _, ns := range base {
		if exclusions[ns] || seen[ns] {
			continue
		}
		seen[ns] = true
		result = append(result, ns)
	}

	sort.Strings(result)
	return result, nil
}
