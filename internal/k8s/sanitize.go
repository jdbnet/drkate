package k8s

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jamie/drkate/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

var defaultExcludedKinds = map[string]bool{
	"Event":              true,
	"Binding":            true,
	"Endpoints":          true,
	"EndpointSlice":      true,
	"PodMetrics":         true,
	"NodeMetrics":        true,
	"ReplicaSet":         true,
	"Pod":                true,
	"ControllerRevision": true,
	"Lease":              true,
}

var defaultExcludedNames = map[string]map[string]bool{
	"ConfigMap": {
		"kube-root-ca.crt": true,
	},
	"ServiceAccount": {
		"default": true,
	},
}

var annotationExactStrip = map[string]bool{
	"kubectl.kubernetes.io/last-applied-configuration": true,
	"deployment.kubernetes.io/revision":                true,
	"kubernetes.io/change-cause":                       true,
	"kubernetes.io/ingress.class":                      true,
}

var annotationPrefixStrip = []string{
	"kubectl.kubernetes.io/",
	"banzaicloud.com/",
	"field.cattle.io/",
}

var labelExactStrip = map[string]bool{
	"pod-template-hash":                true,
	"controller-revision-hash":         true,
	"batch.kubernetes.io/job-tracking": true,
}

var metadataFieldsStrip = []string{
	"resourceVersion",
	"uid",
	"generation",
	"creationTimestamp",
	"deletionTimestamp",
	"deletionGracePeriodSeconds",
	"selfLink",
	"managedFields",
	"ownerReferences",
	"finalizers",
}

type Sanitizer struct {
	excludeKinds            map[string]bool
	excludeNames            map[string]map[string]bool
	preserveHelmAnnotations bool
	preserveReplicas        bool
}

func NewSanitizer(cfg config.ScrapeConfig) *Sanitizer {
	excluded := make(map[string]bool)
	for k, v := range defaultExcludedKinds {
		excluded[k] = v
	}
	for _, k := range cfg.ExcludeKinds {
		excluded[k] = true
	}

	excludedNames := make(map[string]map[string]bool)
	for kind, names := range defaultExcludedNames {
		m := make(map[string]bool, len(names))
		for name := range names {
			m[name] = true
		}
		excludedNames[kind] = m
	}
	for kind, names := range cfg.ExcludeNames {
		m, ok := excludedNames[kind]
		if !ok {
			m = make(map[string]bool)
			excludedNames[kind] = m
		}
		for _, name := range names {
			m[name] = true
		}
	}

	return &Sanitizer{
		excludeKinds:            excluded,
		excludeNames:            excludedNames,
		preserveHelmAnnotations: cfg.PreserveHelmAnnotations,
		preserveReplicas:        cfg.PreserveReplicas,
	}
}

func (s *Sanitizer) IsExcludedKind(kind string) bool {
	return s.excludeKinds[kind]
}

func (s *Sanitizer) IsExcludedResource(kind, name string) bool {
	if names, ok := s.excludeNames[kind]; ok {
		return names[name]
	}
	return false
}

func (s *Sanitizer) SanitizeObject(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	u := obj.DeepCopy()
	u.SetManagedFields(nil)
	delete(u.Object, "status")

	meta := u.Object["metadata"]
	if metaMap, ok := meta.(map[string]interface{}); ok {
		s.sanitizeMetadata(metaMap)
		u.Object["metadata"] = metaMap
	}

	kind := u.GetKind()
	switch kind {
	case "Service":
		s.sanitizeService(u)
	case "Deployment", "StatefulSet", "DaemonSet":
		s.sanitizeWorkload(u)
	case "PersistentVolumeClaim":
		s.sanitizePVC(u)
	case "Ingress":
		s.sanitizeIngress(u)
	case "ConfigMap":
		s.sanitizeConfigMap(u)
	case "Secret":
		s.sanitizeSecret(u)
	case "ServiceAccount":
		s.sanitizeServiceAccount(u)
	case "Job", "CronJob":
		s.sanitizeJob(u)
	}

	s.dropEmptyMaps(u.Object)
	return u, nil
}

func (s *Sanitizer) SanitizeYAML(yamlData []byte) ([]byte, error) {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlData, &obj.Object); err != nil {
		return nil, err
	}
	cleaned, err := s.SanitizeObject(obj)
	if err != nil {
		return nil, err
	}
	return ToOrderedYAML(cleaned)
}

func (s *Sanitizer) sanitizeMetadata(meta map[string]interface{}) {
	for _, f := range metadataFieldsStrip {
		delete(meta, f)
	}

	if labels, ok := meta["labels"].(map[string]interface{}); ok {
		s.stripLabels(labels)
		if len(labels) == 0 {
			delete(meta, "labels")
		}
	}

	if ann, ok := meta["annotations"].(map[string]interface{}); ok {
		s.stripAnnotations(ann)
		if len(ann) == 0 {
			delete(meta, "annotations")
		}
	}
}

func (s *Sanitizer) stripAnnotations(ann map[string]interface{}) {
	for k := range ann {
		if annotationExactStrip[k] {
			delete(ann, k)
			continue
		}
		if !s.preserveHelmAnnotations && strings.HasPrefix(k, "meta.helm.sh/") {
			delete(ann, k)
			continue
		}
		if strings.Contains(k, ".kubernetes.io/reset-") {
			delete(ann, k)
			continue
		}
		if strings.Contains(k, "cattle.io/") {
			delete(ann, k)
			continue
		}
		for _, prefix := range annotationPrefixStrip {
			if strings.HasPrefix(k, prefix) {
				delete(ann, k)
				break
			}
		}
		if strings.HasPrefix(k, "volume.kubernetes.io/") {
			delete(ann, k)
		}
	}
}

func (s *Sanitizer) stripLabels(labels map[string]interface{}) {
	for k := range labels {
		if labelExactStrip[k] {
			delete(labels, k)
			continue
		}
		if strings.Contains(k, "cattle.io/") {
			delete(labels, k)
			continue
		}
		if strings.HasSuffix(k, "-hash") {
			delete(labels, k)
		}
	}
}

func (s *Sanitizer) sanitizeService(u *unstructured.Unstructured) {
	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		return
	}
	delete(spec, "clusterIP")
	delete(spec, "clusterIPs")
	delete(spec, "ipFamilies")
}

func (s *Sanitizer) sanitizeWorkload(u *unstructured.Unstructured) {
	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		return
	}
	if template, ok := spec["template"].(map[string]interface{}); ok {
		if meta, ok := template["metadata"].(map[string]interface{}); ok {
			s.sanitizeMetadata(meta)
			template["metadata"] = meta
		}
		spec["template"] = template
	}
}

func (s *Sanitizer) sanitizePVC(u *unstructured.Unstructured) {
	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		return
	}
	delete(spec, "volumeName")
}

func (s *Sanitizer) sanitizeIngress(u *unstructured.Unstructured) {
	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		return
	}
	delete(spec, "ingressClassName")
}

func (s *Sanitizer) sanitizeConfigMap(u *unstructured.Unstructured) {
	if bd, ok := u.Object["binaryData"].(map[string]interface{}); ok && len(bd) == 0 {
		delete(u.Object, "binaryData")
	}
}

func (s *Sanitizer) sanitizeSecret(u *unstructured.Unstructured) {
	if t, ok := u.Object["type"].(string); ok && t == "" {
		delete(u.Object, "type")
	}
}

func (s *Sanitizer) sanitizeServiceAccount(u *unstructured.Unstructured) {
	delete(u.Object, "secrets")
}

func (s *Sanitizer) sanitizeJob(u *unstructured.Unstructured) {
	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		return
	}
	delete(spec, "completionTime")
	delete(spec, "startTime")
}

func (s *Sanitizer) dropEmptyMaps(m map[string]interface{}) {
	for k, v := range m {
		switch child := v.(type) {
		case map[string]interface{}:
			s.dropEmptyMaps(child)
			if len(child) == 0 {
				delete(m, k)
			}
		}
	}
}

func ToOrderedYAML(u *unstructured.Unstructured) ([]byte, error) {
	preferred := []string{"apiVersion", "kind", "metadata", "spec", "data", "stringData", "type"}
	ordered := make(map[string]interface{})
	used := make(map[string]bool, len(preferred))
	for _, k := range preferred {
		if v, ok := u.Object[k]; ok {
			ordered[k] = v
			used[k] = true
		}
	}
	var extra []string
	for k := range u.Object {
		if used[k] || k == "status" {
			continue
		}
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		ordered[k] = u.Object[k]
	}
	return yaml.Marshal(ordered)
}

func ObjectToYAML(obj runtime.Object) ([]byte, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected unstructured")
	}
	return ToOrderedYAML(u)
}
