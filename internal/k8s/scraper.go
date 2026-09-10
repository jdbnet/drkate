package k8s

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jamie/drkate/internal/config"
	"github.com/jamie/drkate/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

const listTimeout = 45 * time.Second

type Scraper struct {
	source    *ClusterClients
	store     *storage.Store
	sanitizer *Sanitizer
	nsCfg     config.NamespaceConfig
}

type ScrapeResult struct {
	Namespaces    []string `json:"namespaces"`
	ResourceCount int      `json:"resourceCount"`
	Errors        []string `json:"errors,omitempty"`
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
	defer func() {
		if ferr := sc.store.Flush(); ferr != nil {
			log.Printf("scrape index flush: %v", ferr)
		}
		if serr := sc.store.UpdateScrapeStats(namespaces, result.ResourceCount); serr != nil {
			log.Printf("scrape stats: %v", serr)
		}
	}()

	allowed := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		allowed[ns] = struct{}{}
	}

	resourceList, err := sc.source.Discovery.ServerPreferredNamespacedResources()
	if err != nil {
		if len(resourceList) == 0 && !discovery.IsGroupDiscoveryFailedError(err) {
			return result, fmt.Errorf("discover resources: %w", err)
		}
		result.Errors = append(result.Errors, fmt.Sprintf("incomplete API discovery: %v", err))
		log.Printf("scrape: continuing with partial API discovery: %v", err)
	}

	for _, rl := range resourceList {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}
		for _, ar := range rl.APIResources {
			if !ar.Namespaced || strings.Contains(ar.Name, "/") {
				continue
			}
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
			count, errs := sc.scrapeGVR(ctx, gvr, ar.Kind, allowed)
			result.ResourceCount += count
			result.Errors = append(result.Errors, errs...)
			if count > 0 || len(errs) > 0 {
				log.Printf("scrape %s: +%d resources (%d errors, total %d)", ar.Kind, count, len(errs), result.ResourceCount)
			}
		}
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

func (sc *Scraper) scrapeGVR(ctx context.Context, gvr schema.GroupVersionResource, kind string, allowed map[string]struct{}) (int, []string) {
	listCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	list, err := sc.source.Dynamic.Resource(gvr).Namespace(metav1.NamespaceAll).List(listCtx, metav1.ListOptions{})
	if err != nil {
		return 0, []string{fmt.Sprintf("%s/%s: %v", kind, gvr.Resource, err)}
	}

	var count int
	var errors []string
	for _, item := range list.Items {
		if _, ok := allowed[item.GetNamespace()]; !ok {
			continue
		}
		if sc.sanitizer.IsExcludedResource(kind, item.GetName()) {
			if err := sc.store.Delete(item.GetNamespace(), kind, item.GetName()); err != nil {
				errors = append(errors, fmt.Sprintf("drop excluded %s/%s/%s: %v", item.GetNamespace(), kind, item.GetName(), err))
			}
			continue
		}
		cleaned, err := sc.sanitizer.SanitizeObject(&item)
		if err != nil {
			errors = append(errors, fmt.Sprintf("sanitize %s/%s/%s: %v", item.GetNamespace(), kind, item.GetName(), err))
			continue
		}
		yamlData, err := ToOrderedYAML(cleaned)
		if err != nil {
			errors = append(errors, fmt.Sprintf("yaml %s/%s/%s: %v", item.GetNamespace(), kind, item.GetName(), err))
			continue
		}
		meta := storage.ResourceMeta{
			Namespace:       item.GetNamespace(),
			Kind:            kind,
			Name:            item.GetName(),
			APIVersion:      cleaned.GetAPIVersion(),
			Resource:        gvr.Resource,
			ScrapedAt:       time.Now(),
			Warnings:        DetectWarnings(&item),
			WarningsChecked: true,
		}
		if err := sc.store.Save(meta, yamlData); err != nil {
			errors = append(errors, fmt.Sprintf("save %s/%s/%s: %v", item.GetNamespace(), kind, item.GetName(), err))
			continue
		}
		count++
	}
	return count, errors
}
