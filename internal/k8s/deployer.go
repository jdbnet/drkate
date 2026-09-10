package k8s

import (
	"context"
	"fmt"
	"sort"

	"github.com/jamie/drkate/internal/storage"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

var deployPriority = map[string]int{
	"Namespace":               10,
	"ServiceAccount":          20,
	"ConfigMap":               30,
	"Secret":                  31,
	"PersistentVolumeClaim":   40,
	"Service":                 50,
	"Deployment":              60,
	"StatefulSet":             61,
	"DaemonSet":               62,
	"Job":                     63,
	"CronJob":                 64,
	"Ingress":                 70,
	"NetworkPolicy":           71,
	"HorizontalPodAutoscaler": 72,
	"PodDisruptionBudget":     73,
	"Role":                    80,
	"RoleBinding":             81,
}

type DeployRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Filter    string `json:"filter,omitempty"` // missing, drifted, all
}

type DeployResult struct {
	Success []string `json:"success"`
	Failed  []string `json:"failed"`
}

type Deployer struct {
	dr        *ClusterClients
	store     *storage.Store
	sanitizer *Sanitizer
}

func NewDeployer(dr *ClusterClients, store *storage.Store, sanitizer *Sanitizer) *Deployer {
	return &Deployer{dr: dr, store: store, sanitizer: sanitizer}
}

func (d *Deployer) Deploy(ctx context.Context, req DeployRequest, comparator *Comparator) (*DeployResult, error) {
	var targets []storage.ResourceMeta

	if req.Kind != "" && req.Name != "" {
		targets = []storage.ResourceMeta{{Namespace: req.Namespace, Kind: req.Kind, Name: req.Name}}
	} else {
		all := d.store.List(req.Namespace, "")
		for _, r := range all {
			if req.Filter != "" && req.Filter != "all" {
				status, _, err := comparator.compareOne(ctx, r)
				if err != nil || status != ResourceStatus(req.Filter) {
					continue
				}
			}
			targets = append(targets, r)
		}
	}

	sort.Slice(targets, func(i, j int) bool {
		pi := deployPriority[targets[i].Kind]
		pj := deployPriority[targets[j].Kind]
		if pi != pj {
			return pi < pj
		}
		return targets[i].Name < targets[j].Name
	})

	result := &DeployResult{}
	ensured := map[string]error{}
	for _, t := range targets {
		if t.Kind != "Namespace" && t.Namespace != "" {
			err, ok := ensured[t.Namespace]
			if !ok {
				err = d.ensureNamespace(ctx, t.Namespace)
				ensured[t.Namespace] = err
			}
			if err != nil {
				key := fmt.Sprintf("%s/%s/%s", t.Namespace, t.Kind, t.Name)
				result.Failed = append(result.Failed, key+": "+err.Error())
				continue
			}
		}
		key := fmt.Sprintf("%s/%s/%s", t.Namespace, t.Kind, t.Name)
		if err := d.deployOne(ctx, t); err != nil {
			result.Failed = append(result.Failed, key+": "+err.Error())
		} else {
			result.Success = append(result.Success, key)
		}
	}
	return result, nil
}

func (d *Deployer) ensureNamespace(ctx context.Context, name string) error {
	_, err := d.dr.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", name, err)
	}
	ns := &corev1.Namespace{}
	ns.Name = name
	_, err = d.dr.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", name, err)
	}
	return nil
}

func (d *Deployer) deployOne(ctx context.Context, meta storage.ResourceMeta) error {
	yamlData, meta, err := d.store.Get(meta.Namespace, meta.Kind, meta.Name)
	if err != nil {
		return err
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlData, &obj.Object); err != nil {
		return err
	}

	cleaned, err := d.sanitizer.SanitizeObject(obj)
	if err != nil {
		return err
	}

	gvr, err := gvrFromMeta(cleaned, meta)
	if err != nil {
		return err
	}

	name := cleaned.GetName()
	resource := namespacedResource(d.dr.Dynamic, gvr, meta.Kind, cleaned.GetNamespace())

	_, err = resource.Apply(ctx, name, cleaned, metav1.ApplyOptions{
		FieldManager: "drkate",
		Force:        true,
	})
	return err
}

func namespacedResource(client dynamic.Interface, gvr schema.GroupVersionResource, kind, namespace string) dynamic.ResourceInterface {
	res := client.Resource(gvr)
	if isClusterScopedKind(kind) {
		return res
	}
	return res.Namespace(namespace)
}

func isClusterScopedKind(kind string) bool {
	switch kind {
	case "Namespace", "ClusterRole", "ClusterRoleBinding", "PersistentVolume", "StorageClass", "PriorityClass":
		return true
	default:
		return false
	}
}
