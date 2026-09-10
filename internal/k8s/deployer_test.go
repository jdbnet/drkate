package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureNamespaceCreatesMissing(t *testing.T) {
	client := fake.NewSimpleClientset()
	d := &Deployer{dr: &ClusterClients{Clientset: client}}
	if err := d.ensureNamespace(context.Background(), "dlp-builder-staging"); err != nil {
		t.Fatal(err)
	}
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), "dlp-builder-staging", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ns.Name != "dlp-builder-staging" {
		t.Fatalf("got %q", ns.Name)
	}
}

func TestEnsureNamespaceIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "existing"}})
	d := &Deployer{dr: &ClusterClients{Clientset: client}}
	if err := d.ensureNamespace(context.Background(), "existing"); err != nil {
		t.Fatal(err)
	}
}
