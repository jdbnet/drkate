package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamie/drkate/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type ResourceStatus string

const (
	StatusSynced  ResourceStatus = "synced"
	StatusDrifted ResourceStatus = "drifted"
	StatusMissing ResourceStatus = "missing"
)

type ResourceStatusEntry struct {
	Namespace  string         `json:"namespace"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Status     ResourceStatus `json:"status"`
	ScrapedAt  string         `json:"scrapedAt,omitempty"`
	UpdatedAt  string         `json:"updatedAt,omitempty"`
}

type NamespaceStatus struct {
	Namespace string `json:"namespace"`
	Synced    int    `json:"synced"`
	Drifted   int    `json:"drifted"`
	Missing   int    `json:"missing"`
	Total     int    `json:"total"`
}

type Comparator struct {
	dr        *ClusterClients
	store     *storage.Store
	sanitizer *Sanitizer
}

func NewComparator(dr *ClusterClients, store *storage.Store, sanitizer *Sanitizer) *Comparator {
	return &Comparator{dr: dr, store: store, sanitizer: sanitizer}
}

func (c *Comparator) AllNamespaceStatus(ctx context.Context) ([]NamespaceStatus, error) {
	resources := c.store.AllMeta()
	nsMap := make(map[string]*NamespaceStatus)

	for _, r := range resources {
		ns, ok := nsMap[r.Namespace]
		if !ok {
			ns = &NamespaceStatus{Namespace: r.Namespace}
			nsMap[r.Namespace] = ns
		}
		status, err := c.compareOne(ctx, r)
		if err != nil {
			ns.Missing++
			ns.Total++
			continue
		}
		switch status {
		case StatusSynced:
			ns.Synced++
		case StatusDrifted:
			ns.Drifted++
		case StatusMissing:
			ns.Missing++
		}
		ns.Total++
	}

	var out []NamespaceStatus
	for _, ns := range nsMap {
		out = append(out, *ns)
	}
	return out, nil
}

func (c *Comparator) NamespaceDetail(ctx context.Context, namespace string) ([]ResourceStatusEntry, error) {
	resources := c.store.List(namespace, "")
	var out []ResourceStatusEntry
	for _, r := range resources {
		status, err := c.compareOne(ctx, r)
		if err != nil {
			status = StatusMissing
		}
		out = append(out, ResourceStatusEntry{
			Namespace: r.Namespace,
			Kind:      r.Kind,
			Name:      r.Name,
			Status:    status,
			ScrapedAt: r.ScrapedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, nil
}

func (c *Comparator) CompareOne(ctx context.Context, meta storage.ResourceMeta) (ResourceStatus, error) {
	return c.compareOne(ctx, meta)
}

func (c *Comparator) compareOne(ctx context.Context, meta storage.ResourceMeta) (ResourceStatus, error) {
	yamlData, _, err := c.store.Get(meta.Namespace, meta.Kind, meta.Name)
	if err != nil {
		return StatusMissing, err
	}

	stored := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlData, &stored.Object); err != nil {
		return StatusMissing, err
	}

	gvr, err := gvrFromMeta(stored, meta)
	if err != nil {
		return StatusMissing, err
	}

	live, err := c.dr.Dynamic.Resource(gvr).Namespace(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
	if err != nil {
		return StatusMissing, nil
	}

	cleanedLive, err := c.sanitizer.SanitizeObject(live)
	if err != nil {
		return StatusDrifted, nil
	}

	storedSpec := stored.Object["spec"]
	liveSpec := cleanedLive.Object["spec"]

	if specsEqual(storedSpec, liveSpec) {
		return StatusSynced, nil
	}
	return StatusDrifted, nil
}

func specsEqual(a, b interface{}) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func gvrFromMeta(u *unstructured.Unstructured, meta storage.ResourceMeta) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(u.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	resource := meta.Resource
	if resource == "" {
		resource, err = pluralResource(u.GetKind())
		if err != nil {
			return schema.GroupVersionResource{}, err
		}
	}
	return schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: resource,
	}, nil
}

func gvrFromObject(u *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	return gvrFromMeta(u, storage.ResourceMeta{})
}

func pluralResource(kind string) (string, error) {
	mapping := map[string]string{
		"ConfigMap":             "configmaps",
		"Secret":                "secrets",
		"Service":               "services",
		"Deployment":            "deployments",
		"StatefulSet":           "statefulsets",
		"DaemonSet":             "daemonsets",
		"Ingress":               "ingresses",
		"PersistentVolumeClaim": "persistentvolumeclaims",
		"Job":                   "jobs",
		"CronJob":               "cronjobs",
		"HorizontalPodAutoscaler": "horizontalpodautoscalers",
		"NetworkPolicy":         "networkpolicies",
		"ServiceAccount":        "serviceaccounts",
		"Role":                  "roles",
		"RoleBinding":           "rolebindings",
		"PodDisruptionBudget":   "poddisruptionbudgets",
	}
	if r, ok := mapping[kind]; ok {
		return r, nil
	}
	return fmt.Sprintf("%ss", strings.ToLower(kind)), nil
}
