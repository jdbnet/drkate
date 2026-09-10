package k8s

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetectWarningsPVCLonghorn(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "PersistentVolumeClaim",
		"spec": map[string]interface{}{"storageClassName": "longhorn"},
	}}
	got := DetectWarnings(u)
	if len(got) != 1 || got[0].Code != WarningStorage {
		t.Fatalf("got %#v", got)
	}
	if !strings.Contains(got[0].Message, "longhorn") || !strings.Contains(got[0].Message, "cluster-local") {
		t.Fatalf("message=%q", got[0].Message)
	}
}

func TestDetectWarningsHostPathAndPVCMount(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Deployment",
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"volumes": []interface{}{
						map[string]interface{}{
							"name":                  "data",
							"persistentVolumeClaim": map[string]interface{}{"claimName": "app-data"},
						},
						map[string]interface{}{
							"name":     "sock",
							"hostPath": map[string]interface{}{"path": "/var/run/docker.sock"},
						},
						map[string]interface{}{
							"name":     "tmp",
							"emptyDir": map[string]interface{}{},
						},
						map[string]interface{}{
							"name":      "cfg",
							"configMap": map[string]interface{}{"name": "app"},
						},
					},
				},
			},
		},
	}}
	got := DetectWarnings(u)
	if len(got) != 2 {
		t.Fatalf("got %d warnings: %#v", len(got), got)
	}
	joined := got[0].Message + " " + got[1].Message
	if !strings.Contains(joined, "app-data") || !strings.Contains(joined, "/var/run/docker.sock") {
		t.Fatalf("messages=%q", joined)
	}
}

func TestDetectWarningsIngressNginx(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Ingress",
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
				"cert-manager.io/cluster-issuer":             "letsencrypt",
			},
		},
		"spec": map[string]interface{}{
			"ingressClassName": "nginx",
		},
	}}
	got := DetectWarnings(u)
	if len(got) != 3 {
		t.Fatalf("got %d warnings: %#v", len(got), got)
	}
	for _, w := range got {
		if w.Code != WarningIngress {
			t.Fatalf("code=%s", w.Code)
		}
	}
}

func TestDetectWarningsConfigMapSilent(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "ConfigMap",
		"data": map[string]interface{}{"k": "v"},
	}}
	if got := DetectWarnings(u); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestDetectWarningsStatefulSetClaimTemplate(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "StatefulSet",
		"spec": map[string]interface{}{
			"volumeClaimTemplates": []interface{}{
				map[string]interface{}{
					"metadata": map[string]interface{}{"name": "data"},
					"spec":     map[string]interface{}{"storageClassName": "local-path"},
				},
			},
		},
	}}
	got := DetectWarnings(u)
	if len(got) != 1 || got[0].Code != WarningStorage {
		t.Fatalf("got %#v", got)
	}
	if !strings.Contains(got[0].Message, "local-path") {
		t.Fatalf("message=%q", got[0].Message)
	}
}
