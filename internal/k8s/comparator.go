package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/jamie/drkate/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/yaml"
)

type ResourceStatus string

const (
	StatusSynced  ResourceStatus = "synced"
	StatusDrifted ResourceStatus = "drifted"
	StatusMissing ResourceStatus = "missing"
	StatusPending ResourceStatus = "pending"
)

type ResourceStatusEntry struct {
	Namespace string                    `json:"namespace"`
	Kind      string                    `json:"kind"`
	Name      string                    `json:"name"`
	Status    ResourceStatus            `json:"status"`
	Drift     string                    `json:"drift,omitempty"`
	ScrapedAt string                    `json:"scrapedAt,omitempty"`
	UpdatedAt string                    `json:"updatedAt,omitempty"`
	Warnings  []storage.ResourceWarning `json:"warnings,omitempty"`
	Edited    bool                      `json:"edited,omitempty"`
}

type resourceVerdict struct {
	status   ResourceStatus
	drift    string
	warnings []storage.ResourceWarning
}

type NamespaceStatus struct {
	Namespace    string `json:"namespace"`
	Synced       int    `json:"synced"`
	Drifted      int    `json:"drifted"`
	Missing      int    `json:"missing"`
	Total        int    `json:"total"`
	WarningCount int    `json:"warningCount"`
}

type Comparator struct {
	dr        *ClusterClients
	store     *storage.Store
	sanitizer *Sanitizer
}

func NewComparator(dr *ClusterClients, store *storage.Store, sanitizer *Sanitizer) *Comparator {
	return &Comparator{dr: dr, store: store, sanitizer: sanitizer}
}

func (c *Comparator) Snapshot(ctx context.Context) (*DRStatusSnapshot, error) {
	resources := c.store.AllMeta()
	live := c.loadLive(ctx, resources)
	var warningUpdates []storage.WarningUpdate
	snap := c.buildSnapshot(func(r storage.ResourceMeta) resourceVerdict {
		warnings := r.Warnings
		var yamlData []byte
		if !r.WarningsChecked {
			data, _, err := c.store.Get(r.Namespace, r.Kind, r.Name)
			if err == nil {
				yamlData = data
				warnings = DetectWarningsFromYAML(data)
				warningUpdates = append(warningUpdates, storage.WarningUpdate{
					Namespace: r.Namespace,
					Kind:      r.Kind,
					Name:      r.Name,
					Warnings:  warnings,
				})
			}
		}
		obj, ok := live[liveKey{r.Namespace, r.Kind, r.Name}]
		if !ok {
			return resourceVerdict{status: StatusMissing, warnings: warnings}
		}
		if yamlData == nil {
			var err error
			yamlData, _, err = c.store.Get(r.Namespace, r.Kind, r.Name)
			if err != nil {
				return resourceVerdict{status: StatusMissing, warnings: warnings}
			}
		}
		stored := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(yamlData, &stored.Object); err != nil {
			return resourceVerdict{status: StatusMissing, warnings: warnings}
		}
		status, drift := driftBetween(stored, obj, c.sanitizer)
		return resourceVerdict{status: status, drift: drift, warnings: warnings}
	})
	if err := c.store.ApplyWarnings(warningUpdates); err != nil {
		log.Printf("persist resource warnings: %v", err)
	}
	return snap, nil
}

func (c *Comparator) Inventory() *DRStatusSnapshot {
	return c.buildSnapshot(func(r storage.ResourceMeta) resourceVerdict {
		if r.Status == "" {
			return resourceVerdict{status: StatusPending, warnings: r.Warnings}
		}
		return resourceVerdict{status: ResourceStatus(r.Status), drift: r.Drift, warnings: r.Warnings}
	})
}

func (c *Comparator) PersistSnapshot(snap *DRStatusSnapshot) error {
	if snap == nil {
		return nil
	}
	var updates []storage.StatusUpdate
	for _, entries := range snap.Namespaces {
		for _, e := range entries {
			updates = append(updates, storage.StatusUpdate{
				Namespace: e.Namespace,
				Kind:      e.Kind,
				Name:      e.Name,
				Status:    string(e.Status),
				Drift:     e.Drift,
			})
		}
	}
	return c.store.ApplyStatuses(updates)
}

func (c *Comparator) buildSnapshot(statusOf func(storage.ResourceMeta) resourceVerdict) *DRStatusSnapshot {
	resources := c.store.AllMeta()
	nsMap := make(map[string]*NamespaceStatus)
	nsDetail := make(map[string][]ResourceStatusEntry)

	for _, r := range resources {
		ns, ok := nsMap[r.Namespace]
		if !ok {
			ns = &NamespaceStatus{Namespace: r.Namespace}
			nsMap[r.Namespace] = ns
		}

		verdict := statusOf(r)
		switch verdict.status {
		case StatusSynced:
			ns.Synced++
		case StatusDrifted:
			ns.Drifted++
		case StatusMissing:
			ns.Missing++
		}
		ns.Total++
		warnings := verdict.warnings
		if len(warnings) > 0 {
			ns.WarningCount++
		}

		nsDetail[r.Namespace] = append(nsDetail[r.Namespace], ResourceStatusEntry{
			Namespace: r.Namespace,
			Kind:      r.Kind,
			Name:      r.Name,
			Status:    verdict.status,
			Drift:     verdict.drift,
			ScrapedAt: r.ScrapedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Warnings:  warnings,
			Edited:    r.Edited,
		})
	}

	var overview []NamespaceStatus
	for _, ns := range nsMap {
		overview = append(overview, *ns)
	}

	return &DRStatusSnapshot{
		Overview:   overview,
		Namespaces: nsDetail,
	}
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
		status, _, err := c.compareOne(ctx, r)
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
		status, drift, err := c.compareOne(ctx, r)
		if err != nil {
			status = StatusMissing
			drift = ""
		}
		out = append(out, ResourceStatusEntry{
			Namespace: r.Namespace,
			Kind:      r.Kind,
			Name:      r.Name,
			Status:    status,
			Drift:     drift,
			ScrapedAt: r.ScrapedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Warnings:  r.Warnings,
			Edited:    r.Edited,
		})
	}
	return out, nil
}

func (c *Comparator) EnsureWarnings(namespace string) bool {
	resources := c.store.List(namespace, "")
	var updates []storage.WarningUpdate
	for _, r := range resources {
		if r.WarningsChecked {
			continue
		}
		yamlData, _, err := c.store.Get(r.Namespace, r.Kind, r.Name)
		if err != nil {
			continue
		}
		updates = append(updates, storage.WarningUpdate{
			Namespace: r.Namespace,
			Kind:      r.Kind,
			Name:      r.Name,
			Warnings:  DetectWarningsFromYAML(yamlData),
		})
	}
	if len(updates) == 0 {
		return false
	}
	if err := c.store.ApplyWarnings(updates); err != nil {
		log.Printf("persist resource warnings: %v", err)
		return false
	}
	return true
}

func (c *Comparator) CompareOne(ctx context.Context, meta storage.ResourceMeta) (ResourceStatus, string, error) {
	return c.compareOne(ctx, meta)
}

type liveKey struct {
	ns   string
	kind string
	name string
}

type gvrIdentity struct {
	group    string
	resource string
}

func (c *Comparator) loadLive(ctx context.Context, resources []storage.ResourceMeta) map[liveKey]*unstructured.Unstructured {
	out := make(map[liveKey]*unstructured.Unstructured)
	if c.dr == nil {
		return out
	}

	available, err := c.drPreferredGVRs()
	if err != nil {
		log.Printf("dr compare discovery: %v", err)
	}

	seen := make(map[gvrIdentity]struct{})
	var listed, skipped int
	for _, r := range resources {
		gvr, err := gvrFromResourceMeta(r)
		if err != nil {
			continue
		}
		id := gvrIdentity{gvr.Group, gvr.Resource}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		listGVR, ok := available[id]
		if !ok {
			skipped++
			continue
		}
		if err := ctx.Err(); err != nil {
			return out
		}

		listCtx, cancel := context.WithTimeout(ctx, listTimeout)
		list, err := c.dr.Dynamic.Resource(listGVR).Namespace(metav1.NamespaceAll).List(listCtx, metav1.ListOptions{})
		cancel()
		if err != nil {
			log.Printf("dr compare list %s: %v", listGVR.String(), err)
			continue
		}
		listed++
		for i := range list.Items {
			u := &list.Items[i]
			out[liveKey{u.GetNamespace(), u.GetKind(), u.GetName()}] = u
		}
	}
	log.Printf("dr compare: listed %d types on DR, skipped %d source-only types, %d live objects", listed, skipped, len(out))
	return out
}

func (c *Comparator) drPreferredGVRs() (map[gvrIdentity]schema.GroupVersionResource, error) {
	out := make(map[gvrIdentity]schema.GroupVersionResource)
	if c.dr.Discovery == nil {
		return out, nil
	}

	resourceList, err := c.dr.Discovery.ServerPreferredNamespacedResources()
	if err != nil {
		if len(resourceList) == 0 && !discovery.IsGroupDiscoveryFailedError(err) {
			return out, err
		}
		log.Printf("dr compare: continuing with partial API discovery: %v", err)
	}

	for _, rl := range resourceList {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		for _, ar := range rl.APIResources {
			if !ar.Namespaced || strings.Contains(ar.Name, "/") || !hasVerb(ar.Verbs, "list") {
				continue
			}
			id := gvrIdentity{gv.Group, ar.Name}
			out[id] = schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: ar.Name,
			}
		}
	}
	return out, nil
}

func gvrFromResourceMeta(meta storage.ResourceMeta) (schema.GroupVersionResource, error) {
	if meta.APIVersion == "" {
		return schema.GroupVersionResource{}, fmt.Errorf("missing apiVersion for %s/%s/%s", meta.Namespace, meta.Kind, meta.Name)
	}
	gv, err := schema.ParseGroupVersion(meta.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	resource := meta.Resource
	if resource == "" {
		resource, err = pluralResource(meta.Kind)
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

func (c *Comparator) compareOne(ctx context.Context, meta storage.ResourceMeta) (ResourceStatus, string, error) {
	yamlData, _, err := c.store.Get(meta.Namespace, meta.Kind, meta.Name)
	if err != nil {
		return StatusMissing, "", err
	}

	stored := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlData, &stored.Object); err != nil {
		return StatusMissing, "", err
	}

	gvr, err := gvrFromMeta(stored, meta)
	if err != nil {
		return StatusMissing, "", err
	}

	live, err := c.dr.Dynamic.Resource(gvr).Namespace(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
	if err != nil {
		return StatusMissing, "", nil
	}

	status, drift := driftBetween(stored, live, c.sanitizer)
	return status, drift, nil
}

func driftBetween(stored, live *unstructured.Unstructured, sanitizer *Sanitizer) (ResourceStatus, string) {
	storedClean, err := sanitizer.SanitizeObject(stored)
	if err != nil {
		return StatusDrifted, "stored manifest"
	}
	liveClean, err := sanitizer.SanitizeObject(live)
	if err != nil {
		return StatusDrifted, "live object"
	}

	desired := compareView(storedClean)
	actual := compareView(liveClean)
	var paths []string
	diffPaths(desired, actual, "", &paths)
	if len(paths) == 0 {
		return StatusSynced, ""
	}
	return StatusDrifted, strings.Join(paths, ", ")
}

func compareView(u *unstructured.Unstructured) map[string]interface{} {
	out := map[string]interface{}{}
	skip := map[string]bool{
		"apiVersion": true,
		"kind":       true,
		"metadata":   true,
		"status":     true,
	}
	for k, v := range u.Object {
		if skip[k] {
			continue
		}
		out[k] = v
	}
	if meta, ok := u.Object["metadata"].(map[string]interface{}); ok {
		if labels, ok := meta["labels"]; ok {
			out["metadata.labels"] = labels
		}
		if ann, ok := meta["annotations"]; ok {
			out["metadata.annotations"] = ann
		}
	}
	return out
}

const maxDriftPaths = 6

func diffPaths(a, b interface{}, path string, out *[]string) {
	if len(*out) >= maxDriftPaths {
		return
	}
	if jsonEqual(a, b) {
		return
	}
	am, aok := asMap(a)
	bm, bok := asMap(b)
	if aok && bok {
		keys := make(map[string]struct{}, len(am)+len(bm))
		for k := range am {
			keys[k] = struct{}{}
		}
		for k := range bm {
			keys[k] = struct{}{}
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if len(*out) >= maxDriftPaths {
				return
			}
			child := k
			if path != "" {
				child = path + "." + k
			}
			av, ahave := am[k]
			bv, bhave := bm[k]
			if !ahave || !bhave {
				*out = append(*out, child)
				continue
			}
			diffPaths(av, bv, child, out)
		}
		return
	}
	if path == "" {
		path = "(root)"
	}
	*out = append(*out, path)
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

func jsonEqual(a, b interface{}) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(aj, bj)
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
		"ConfigMap":               "configmaps",
		"Secret":                  "secrets",
		"Service":                 "services",
		"Deployment":              "deployments",
		"StatefulSet":             "statefulsets",
		"DaemonSet":               "daemonsets",
		"Ingress":                 "ingresses",
		"PersistentVolumeClaim":   "persistentvolumeclaims",
		"Job":                     "jobs",
		"CronJob":                 "cronjobs",
		"HorizontalPodAutoscaler": "horizontalpodautoscalers",
		"NetworkPolicy":           "networkpolicies",
		"ServiceAccount":          "serviceaccounts",
		"Role":                    "roles",
		"RoleBinding":             "rolebindings",
		"PodDisruptionBudget":     "poddisruptionbudgets",
	}
	if r, ok := mapping[kind]; ok {
		return r, nil
	}
	return fmt.Sprintf("%ss", strings.ToLower(kind)), nil
}
