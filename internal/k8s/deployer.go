package k8s

import (
	"context"
	"fmt"
	"sort"

	"github.com/jamie/drkate/internal/storage"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var deployPriority = map[string]int{
	"Namespace":             10,
	"ServiceAccount":        20,
	"ConfigMap":             30,
	"Secret":                31,
	"PersistentVolumeClaim": 40,
	"Service":               50,
	"Deployment":            60,
	"StatefulSet":           61,
	"DaemonSet":             62,
	"Job":                   63,
	"CronJob":               64,
	"Ingress":               70,
	"NetworkPolicy":         71,
	"HorizontalPodAutoscaler": 72,
	"PodDisruptionBudget":   73,
	"Role":                  80,
	"RoleBinding":           81,
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
	dr    *ClusterClients
	store *storage.Store
}

func NewDeployer(dr *ClusterClients, store *storage.Store) *Deployer {
	return &Deployer{dr: dr, store: store}
}

func (d *Deployer) Deploy(ctx context.Context, req DeployRequest, comparator *Comparator) (*DeployResult, error) {
	var targets []storage.ResourceMeta

	if req.Kind != "" && req.Name != "" {
		targets = []storage.ResourceMeta{{Namespace: req.Namespace, Kind: req.Kind, Name: req.Name}}
	} else {
		all := d.store.List(req.Namespace, "")
		for _, r := range all {
			if req.Filter != "" && req.Filter != "all" {
				status, err := comparator.compareOne(ctx, r)
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
	for _, t := range targets {
		key := fmt.Sprintf("%s/%s/%s", t.Namespace, t.Kind, t.Name)
		if err := d.deployOne(ctx, t); err != nil {
			result.Failed = append(result.Failed, key+": "+err.Error())
		} else {
			result.Success = append(result.Success, key)
		}
	}
	return result, nil
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

	gvr, err := gvrFromMeta(obj, meta)
	if err != nil {
		return err
	}

	ns := obj.GetNamespace()
	name := obj.GetName()
	resource := d.dr.Dynamic.Resource(gvr).Namespace(ns)

	existing, err := resource.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = resource.Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	patch, err := yaml.Marshal(obj.Object)
	if err != nil {
		return err
	}
	_, err = resource.Patch(ctx, name, types.ApplyPatchType, patch, metav1.PatchOptions{
		FieldManager: "drkate",
	})
	return err
}
