package k8s

import (
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
			"data": map[string]interface{}{"k": "v"},
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
