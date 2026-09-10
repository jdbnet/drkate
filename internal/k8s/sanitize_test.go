package k8s

import (
	"strings"
	"testing"

	"github.com/jamie/drkate/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSanitizerStripsStatus(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{PreserveReplicas: true})
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":            "test",
				"namespace":       "default",
				"resourceVersion": "123",
				"uid":             "abc",
				"managedFields":   []interface{}{map[string]interface{}{"manager": "kubectl"}},
			},
			"data":   map[string]interface{}{"k": "v"},
			"status": map[string]interface{}{"foo": "bar"},
		},
	}
	out, err := s.SanitizeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Object["status"]; ok {
		t.Fatal("status should be stripped")
	}
	meta := out.Object["metadata"].(map[string]interface{})
	if _, ok := meta["resourceVersion"]; ok {
		t.Fatal("resourceVersion should be stripped")
	}
}

func TestSanitizerExclusions(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})

	for _, kind := range []string{"EndpointSlice", "PodMetrics", "Lease"} {
		if !s.IsExcludedKind(kind) {
			t.Fatalf("expected kind %s to be excluded", kind)
		}
	}

	if !s.IsExcludedResource("ConfigMap", "kube-root-ca.crt") {
		t.Fatal("expected ConfigMap/kube-root-ca.crt to be excluded")
	}
	if !s.IsExcludedResource("ServiceAccount", "default") {
		t.Fatal("expected ServiceAccount/default to be excluded")
	}
	if s.IsExcludedResource("ServiceAccount", "my-app") {
		t.Fatal("expected ServiceAccount/my-app not to be excluded")
	}
	if s.IsExcludedResource("ConfigMap", "app-config") {
		t.Fatal("expected ConfigMap/app-config not to be excluded")
	}
}

func TestSanitizerStripsCattlePublicEndpoints(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "dlp-builder-ingress",
				"namespace": "dlp-builder-staging",
				"annotations": map[string]interface{}{
					"field.cattle.io/publicEndpoints":            `[{"addresses":["10.0.12.46"]}]`,
					"nginx.ingress.kubernetes.io/rewrite-target": "/",
				},
			},
			"spec": map[string]interface{}{
				"ingressClassName": "nginx",
			},
		},
	}
	out, err := s.SanitizeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	meta := out.Object["metadata"].(map[string]interface{})
	ann := meta["annotations"].(map[string]interface{})
	if _, ok := ann["field.cattle.io/publicEndpoints"]; ok {
		t.Fatal("cattle publicEndpoints should be stripped")
	}
	if ann["nginx.ingress.kubernetes.io/rewrite-target"] != "/" {
		t.Fatal("ingress annotation should be kept")
	}
	if spec, ok := out.Object["spec"].(map[string]interface{}); ok {
		if _, hasClass := spec["ingressClassName"]; hasClass {
			t.Fatal("ingressClassName should be stripped")
		}
	}
}

func TestSanitizerServiceClusterIP(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "svc",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"clusterIP":  "10.0.0.1",
				"clusterIPs": []interface{}{"10.0.0.1"},
				"ports":      []interface{}{map[string]interface{}{"port": int64(80)}},
			},
		},
	}
	out, err := s.SanitizeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	spec := out.Object["spec"].(map[string]interface{})
	if _, ok := spec["clusterIP"]; ok {
		t.Fatal("clusterIP should be stripped")
	}
}

func TestSanitizerServiceAccountStripsTokenSecrets(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]interface{}{
				"name":      "my-app",
				"namespace": "default",
			},
			"secrets": []interface{}{
				map[string]interface{}{"name": "my-app-token-abc"},
			},
			"imagePullSecrets": []interface{}{
				map[string]interface{}{"name": "regcred"},
			},
			"automountServiceAccountToken": false,
		},
	}
	out, err := s.SanitizeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Object["secrets"]; ok {
		t.Fatal("token secrets should be stripped")
	}
	if _, ok := out.Object["imagePullSecrets"]; !ok {
		t.Fatal("imagePullSecrets should be kept")
	}
	yamlData, err := ToOrderedYAML(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(yamlData)
	if !strings.Contains(text, "imagePullSecrets") {
		t.Fatalf("expected imagePullSecrets in yaml, got %s", text)
	}
	if strings.Contains(text, "my-app-token-abc") {
		t.Fatalf("did not expect token secret in yaml, got %s", text)
	}
}
