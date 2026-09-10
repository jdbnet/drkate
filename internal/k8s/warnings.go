package k8s

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jamie/drkate/internal/storage"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const (
	WarningStorage = "storage"
	WarningIngress = "ingress"
)

var localStorageClassHints = []string{
	"local-path", "localpath", "hostpath", "local-storage", "local",
	"longhorn", "openebs-hostpath", "openebs-localpv", "openebs-jiva",
	"rawfile", "lvm", "zfspv", "zfs-localpv", "mayastor",
	"microk8s-hostpath", "rancher-local", "sig-storage-local",
}

var ingressAnnotationFamilies = []struct {
	label    string
	prefixes []string
}{
	{label: "nginx", prefixes: []string{"nginx.ingress.kubernetes.io/", "nginx.org/"}},
	{label: "traefik", prefixes: []string{"traefik.ingress.kubernetes.io/", "traefik.containo.us/"}},
	{label: "AWS ALB", prefixes: []string{"alb.ingress.kubernetes.io/"}},
	{label: "HAProxy", prefixes: []string{"haproxy.org/", "haproxy.router.openshift.io/"}},
	{label: "Kong", prefixes: []string{"konghq.com/"}},
	{label: "Contour", prefixes: []string{"projectcontour.io/", "contour.heptio.com/"}},
	{label: "GCE", prefixes: []string{"kubernetes.io/ingress.global-static-ip-name", "kubernetes.io/ingress.allow-http"}},
	{label: "cert-manager", prefixes: []string{"cert-manager.io/", "acme.cert-manager.io/"}},
}

var ingressCRDKinds = map[string]string{
	"IngressRoute":    "Traefik IngressRoute",
	"IngressRouteTCP": "Traefik IngressRouteTCP",
	"IngressRouteUDP": "Traefik IngressRouteUDP",
	"Middleware":      "Traefik Middleware",
	"HTTPProxy":       "Contour HTTPProxy",
	"VirtualService":  "Istio VirtualService",
	"Gateway":         "Gateway API Gateway",
	"HTTPRoute":       "Gateway API HTTPRoute",
	"GRPCRoute":       "Gateway API GRPCRoute",
	"TLSRoute":        "Gateway API TLSRoute",
	"TCPRoute":        "Gateway API TCPRoute",
}

var volumeSourceMessages = map[string]string{
	"hostPath":             "hostPath volume",
	"nfs":                  "NFS volume",
	"iscsi":                "iSCSI volume",
	"fc":                   "Fibre Channel volume",
	"rbd":                  "RBD volume",
	"cephfs":               "CephFS volume",
	"glusterfs":            "GlusterFS volume",
	"cinder":               "Cinder volume",
	"vsphereVolume":        "vSphere volume",
	"azureDisk":            "Azure Disk volume",
	"azureFile":            "Azure File volume",
	"awsElasticBlockStore": "AWS EBS volume",
	"gcePersistentDisk":    "GCE Persistent Disk volume",
	"portworxVolume":       "Portworx volume",
	"flexVolume":           "flexVolume",
	"local":                "local volume",
	"storageos":            "StorageOS volume",
	"quobyte":              "Quobyte volume",
	"photonPersistentDisk": "Photon Persistent Disk volume",
	"scaleIO":              "ScaleIO volume",
	"flocker":              "Flocker volume",
}

var skippedVolumeSources = map[string]bool{
	"name":                true,
	"emptyDir":            true,
	"configMap":           true,
	"secret":              true,
	"projected":           true,
	"downwardAPI":         true,
	"serviceAccountToken": true,
}

func DetectWarningsFromYAML(yamlData []byte) []storage.ResourceWarning {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlData, &obj.Object); err != nil || obj.Object == nil {
		return nil
	}
	return DetectWarnings(obj)
}

func DetectWarnings(u *unstructured.Unstructured) []storage.ResourceWarning {
	if u == nil || u.Object == nil {
		return nil
	}
	var out []storage.ResourceWarning
	kind := u.GetKind()
	out = append(out, pvcWarnings(u, kind)...)
	out = append(out, volumeWarnings(u)...)
	out = append(out, claimTemplateWarnings(u)...)
	out = append(out, ingressWarnings(u, kind)...)
	return uniqWarnings(out)
}

func pvcWarnings(u *unstructured.Unstructured, kind string) []storage.ResourceWarning {
	if kind != "PersistentVolumeClaim" {
		return nil
	}
	sc, _, _ := unstructured.NestedString(u.Object, "spec", "storageClassName")
	return []storage.ResourceWarning{{
		Code:    WarningStorage,
		Message: storageClassMessage("PVC", sc),
	}}
}

func storageClassMessage(what, sc string) string {
	if sc == "" {
		return what + " uses the cluster default storage class. Volume data is not copied on deploy."
	}
	if isLocalStorageClass(sc) {
		return fmt.Sprintf("%s uses cluster-local storage class %q. Volume data is not copied on deploy.", what, sc)
	}
	return fmt.Sprintf("%s uses storage class %q. Volume data is not copied on deploy.", what, sc)
}

func isLocalStorageClass(sc string) bool {
	lower := strings.ToLower(sc)
	for _, hint := range localStorageClassHints {
		if lower == hint || strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func volumeWarnings(u *unstructured.Unstructured) []storage.ResourceWarning {
	var out []storage.ResourceWarning
	for _, vols := range [][]interface{}{
		nestedSlice(u.Object, "spec", "volumes"),
		nestedSlice(u.Object, "spec", "template", "spec", "volumes"),
		nestedSlice(u.Object, "spec", "jobTemplate", "spec", "template", "spec", "volumes"),
	} {
		for _, raw := range vols {
			vol, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := vol["name"].(string)
			if msg := describeVolume(name, vol); msg != "" {
				out = append(out, storage.ResourceWarning{Code: WarningStorage, Message: msg})
			}
		}
	}
	return out
}

func describeVolume(name string, vol map[string]interface{}) string {
	label := "volume"
	if name != "" {
		label = "volume " + name
	}
	if pvc, ok := vol["persistentVolumeClaim"].(map[string]interface{}); ok {
		claim, _ := pvc["claimName"].(string)
		if claim == "" {
			return label + " mounts a PersistentVolumeClaim. Volume data is not copied on deploy."
		}
		return fmt.Sprintf("%s mounts PVC %q. Volume data is not copied on deploy.", label, claim)
	}
	if csi, ok := vol["csi"].(map[string]interface{}); ok {
		driver, _ := csi["driver"].(string)
		if driver == "" {
			return label + " uses a CSI volume. The driver and data may not exist on DR."
		}
		lower := strings.ToLower(driver)
		if isLocalStorageClass(driver) || strings.Contains(lower, "longhorn") || strings.Contains(lower, "lvm") {
			return fmt.Sprintf("%s uses CSI driver %q, which is typically cluster-local.", label, driver)
		}
		return fmt.Sprintf("%s uses CSI driver %q. Confirm it exists on DR.", label, driver)
	}
	if ephemeral, ok := vol["ephemeral"].(map[string]interface{}); ok {
		sc := nestedPathString(ephemeral, "volumeClaimTemplate", "spec", "storageClassName")
		return storageClassMessage(label+" (ephemeral PVC)", sc)
	}
	for key, msg := range volumeSourceMessages {
		if _, ok := vol[key]; ok {
			detail := msg
			if key == "hostPath" {
				if hp, ok := vol["hostPath"].(map[string]interface{}); ok {
					if p, _ := hp["path"].(string); p != "" {
						detail = fmt.Sprintf("hostPath %q", p)
					}
				}
			}
			return fmt.Sprintf("%s uses %s, which will not match the DR node or cluster.", label, detail)
		}
	}
	for key := range vol {
		if skippedVolumeSources[key] {
			continue
		}
		return fmt.Sprintf("%s uses volume source %q. Confirm it is valid on DR.", label, key)
	}
	return ""
}

func claimTemplateWarnings(u *unstructured.Unstructured) []storage.ResourceWarning {
	templates := nestedSlice(u.Object, "spec", "volumeClaimTemplates")
	if len(templates) == 0 {
		return nil
	}
	var out []storage.ResourceWarning
	for _, raw := range templates {
		tmpl, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name := nestedPathString(tmpl, "metadata", "name")
		sc := nestedPathString(tmpl, "spec", "storageClassName")
		what := "StatefulSet volume claim template"
		if name != "" {
			what = fmt.Sprintf("volume claim template %q", name)
		}
		out = append(out, storage.ResourceWarning{
			Code:    WarningStorage,
			Message: storageClassMessage(what, sc),
		})
	}
	return out
}

func ingressWarnings(u *unstructured.Unstructured, kind string) []storage.ResourceWarning {
	var out []storage.ResourceWarning
	if label, ok := ingressCRDKinds[kind]; ok {
		out = append(out, storage.ResourceWarning{
			Code:    WarningIngress,
			Message: label + " is controller-specific and may not exist on DR.",
		})
	}
	if kind == "Ingress" {
		if class, ok, _ := unstructured.NestedString(u.Object, "spec", "ingressClassName"); ok && class != "" {
			out = append(out, storage.ResourceWarning{
				Code:    WarningIngress,
				Message: fmt.Sprintf("Ingress class is %q. This is stripped on scrape so DR can use its own controller.", class),
			})
		}
	}
	ann := u.GetAnnotations()
	if class := ann["kubernetes.io/ingress.class"]; class != "" && kind == "Ingress" {
		out = append(out, storage.ResourceWarning{
			Code:    WarningIngress,
			Message: fmt.Sprintf("Ingress annotation class is %q. This is stripped on scrape so DR can use its own controller.", class),
		})
	}
	for _, family := range ingressAnnotationFamilies {
		if annotationFamilyPresent(ann, family.prefixes) {
			out = append(out, storage.ResourceWarning{
				Code:    WarningIngress,
				Message: fmt.Sprintf("%s ingress annotations are controller-specific and may not apply on DR.", family.label),
			})
		}
	}
	return out
}

func annotationFamilyPresent(ann map[string]string, prefixes []string) bool {
	if len(ann) == 0 {
		return false
	}
	for key := range ann {
		for _, prefix := range prefixes {
			if key == prefix || strings.HasPrefix(key, prefix) {
				return true
			}
		}
	}
	return false
}

func nestedSlice(obj map[string]interface{}, keys ...string) []interface{} {
	v, ok, err := unstructured.NestedSlice(obj, keys...)
	if err != nil || !ok {
		return nil
	}
	return v
}

func nestedPathString(obj map[string]interface{}, keys ...string) string {
	v, ok, err := unstructured.NestedString(obj, keys...)
	if err != nil || !ok {
		return ""
	}
	return v
}

func uniqWarnings(in []storage.ResourceWarning) []storage.ResourceWarning {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []storage.ResourceWarning
	for _, w := range in {
		key := w.Code + "\x00" + w.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code == out[j].Code {
			return out[i].Message < out[j].Message
		}
		return out[i].Code < out[j].Code
	})
	return out
}
