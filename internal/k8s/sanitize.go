package k8s

import (
	"fmt"
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
	"ReplicaSet":         true,
	"Pod":                true,
	"ControllerRevision": true,
}

var annotationExactStrip = map[string]bool{
	"kubectl.kubernetes.io/last-applied-configuration": true,
	"deployment.kubernetes.io/revision":                true,
	"kubernetes.io/change-cause":                       true,
}

var annotationPrefixStrip = []string{
	"kubectl.kubernetes.io/",
	"banzaicloud.com/",
}

var labelExactStrip = map[string]bool{
	"pod-template-hash":              true,
	"controller-revision-hash":       true,
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
	return &Sanitizer{
		excludeKinds:            excluded,
		preserveHelmAnnotations: cfg.PreserveHelmAnnotations,
		preserveReplicas:        cfg.PreserveReplicas,
	}
}

func (s *Sanitizer) IsExcludedKind(kind string) bool {
	return s.excludeKinds[kind]
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
	case "ConfigMap":
		s.sanitizeConfigMap(u)
	case "Secret":
		s.sanitizeSecret(u)
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
	ordered := make(map[string]interface{})
	if v, ok := u.Object["apiVersion"]; ok {
		ordered["apiVersion"] = v
	}
	if v, ok := u.Object["kind"]; ok {
		ordered["kind"] = v
	}
	if v, ok := u.Object["metadata"]; ok {
		ordered["metadata"] = v
	}
	if v, ok := u.Object["spec"]; ok {
		ordered["spec"] = v
	}
	if v, ok := u.Object["data"]; ok {
		ordered["data"] = v
	}
	if v, ok := u.Object["stringData"]; ok {
		ordered["stringData"] = v
	}
	if v, ok := u.Object["type"]; ok {
		ordered["type"] = v
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
