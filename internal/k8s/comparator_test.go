package k8s

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jamie/drkate/internal/config"
	"github.com/jamie/drkate/internal/crypto"
	"github.com/jamie/drkate/internal/storage"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSnapshotMarksMissingWithoutDRGets(t *testing.T) {
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewStore(t.TempDir(), enc)
	if err != nil {
		t.Fatal(err)
	}
	meta := storage.ResourceMeta{
		Namespace:  "ns",
		Kind:       "ConfigMap",
		Name:       "app",
		APIVersion: "v1",
		Resource:   "configmaps",
	}
	if err := store.Save(meta, []byte("apiVersion: v1\nkind: ConfigMap\n")); err != nil {
		t.Fatal(err)
	}

	c := NewComparator(nil, store, NewSanitizer(config.ScrapeConfig{}))
	snap, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entries := snap.Namespaces["ns"]
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].Status != StatusMissing {
		t.Fatalf("got %s, want missing", entries[0].Status)
	}
}

func TestDriftIgnoresCattlePublicEndpoints(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	stored := ingressWithEndpoints("10.0.12.46")
	live := ingressWithEndpoints("10.0.1.1")
	status, drift := driftBetween(stored, live, s)
	if status != StatusSynced {
		t.Fatalf("expected synced, got %s (%s)", status, drift)
	}
}

func TestDriftReportsConfigMapData(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	stored := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "app-config",
				"namespace": "default",
			},
			"data": map[string]interface{}{"FOO": "bar"},
		},
	}
	live := stored.DeepCopy()
	live.Object["data"] = map[string]interface{}{"FOO": "baz"}
	status, drift := driftBetween(stored, live, s)
	if status != StatusDrifted {
		t.Fatalf("expected drifted, got %s", status)
	}
	if !strings.Contains(drift, "data.FOO") {
		t.Fatalf("expected data.FOO in drift, got %q", drift)
	}
}

func TestDriftIgnoresIngressClassName(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	stored := ingressWithClass("nginx")
	live := ingressWithClass("traefik")
	status, drift := driftBetween(stored, live, s)
	if status != StatusSynced {
		t.Fatalf("expected synced, got %s (%s)", status, drift)
	}
}

func TestDriftIgnoresServiceAccountTokenSecrets(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	stored := serviceAccount("src-token-1", "regcred")
	live := serviceAccount("dr-token-9", "regcred")
	status, drift := driftBetween(stored, live, s)
	if status != StatusSynced {
		t.Fatalf("expected synced, got %s (%s)", status, drift)
	}
}

func TestDriftReportsServiceAccountImagePullSecrets(t *testing.T) {
	s := NewSanitizer(config.ScrapeConfig{})
	stored := serviceAccount("src-token-1", "regcred")
	live := serviceAccount("src-token-1", "other-cred")
	status, drift := driftBetween(stored, live, s)
	if status != StatusDrifted {
		t.Fatalf("expected drifted, got %s", status)
	}
	if !strings.Contains(drift, "imagePullSecrets") {
		t.Fatalf("expected imagePullSecrets in drift, got %q", drift)
	}
}

func serviceAccount(token, pullSecret string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]interface{}{
				"name":      "my-app",
				"namespace": "default",
			},
			"secrets": []interface{}{
				map[string]interface{}{"name": token},
			},
			"imagePullSecrets": []interface{}{
				map[string]interface{}{"name": pullSecret},
			},
		},
	}
}

func ingressWithClass(class string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "app",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"ingressClassName": class,
				"rules": []interface{}{
					map[string]interface{}{"host": "app.example.com"},
				},
			},
		},
	}
}

func ingressWithEndpoints(addr string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "dlp-builder-ingress",
				"namespace": "dlp-builder-staging",
				"annotations": map[string]interface{}{
					"field.cattle.io/publicEndpoints": `[{"addresses":["` + addr + `"]}]`,
				},
			},
			"spec": map[string]interface{}{
				"ingressClassName": "nginx",
			},
		},
	}
}
