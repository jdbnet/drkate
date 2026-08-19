package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jamie/drkate/internal/config"
	"github.com/jamie/drkate/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Scraper struct {
	source   *ClusterClients
	store    *storage.Store
	sanitizer *Sanitizer
	nsCfg    config.NamespaceConfig
}

type ScrapeResult struct {
	Namespaces     []string `json:"namespaces"`
	ResourceCount  int      `json:"resourceCount"`
	Errors         []string `json:"errors,omitempty"`
}

func NewScraper(source *ClusterClients, store *storage.Store, scrapeCfg config.ScrapeConfig, nsCfg config.NamespaceConfig) *Scraper {
	return &Scraper{
		source:    source,
		store:     store,
		sanitizer: NewSanitizer(scrapeCfg),
		nsCfg:     nsCfg,
	}
}

func (sc *Scraper) Run(ctx context.Context) (*ScrapeResult, error) {
	namespaces, err := ResolveNamespaces(ctx, sc.nsCfg, sc.source)
	if err != nil {
		return nil, err
	}

	result := &ScrapeResult{Namespaces: namespaces}
	resourceList, err := sc.source.Discovery.ServerPreferredNamespacedResources()
	if err != nil {
		return nil, fmt.Errorf("discover resources: %w", err)
	}

	for _, rl := range resourceList {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		for _, ar := range rl.APIResources {
			if ar.Namespaced && !strings.Contains(ar.Name, "/") {
				if !hasVerb(ar.Verbs, "list") {
					continue
				}
				if sc.sanitizer.IsExcludedKind(ar.Kind) {
					continue
				}
				gvr := schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: ar.Name,
				}
				count, errs := sc.scrapeGVR(ctx, gvr, ar.Kind, namespaces)
				result.ResourceCount += count
				result.Errors = append(result.Errors, errs...)
			}
		}
	}

	if err := sc.store.UpdateScrapeStats(namespaces, result.ResourceCount); err != nil {
		return result, err
	}
	return result, nil
}

func hasVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

func (sc *Scraper) scrapeGVR(ctx context.Context, gvr schema.GroupVersionResource, kind string, namespaces []string) (int, []string) {
	var count int
	var errors []string

	for _, ns := range namespaces {
		list, err := sc.source.Dynamic.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s/%s in %s: %v", kind, gvr.Resource, ns, err))
			continue
		}
		for _, item := range list.Items {
			cleaned, err := sc.sanitizer.SanitizeObject(&item)
			if err != nil {
				errors = append(errors, fmt.Sprintf("sanitize %s/%s: %v", kind, item.GetName(), err))
				continue
			}
			yamlData, err := ToOrderedYAML(cleaned)
			if err != nil {
				errors = append(errors, fmt.Sprintf("yaml %s/%s: %v", kind, item.GetName(), err))
				continue
			}
			meta := storage.ResourceMeta{
				Namespace:  item.GetNamespace(),
				Kind:       kind,
				Name:       item.GetName(),
				APIVersion: cleaned.GetAPIVersion(),
				Resource:   gvr.Resource,
				ScrapedAt:  time.Now(),
			}
			if err := sc.store.Save(meta, yamlData); err != nil {
				errors = append(errors, fmt.Sprintf("save %s/%s: %v", kind, item.GetName(), err))
				continue
			}
			count++
		}
	}
	return count, errors
}
