package k8s

import (
	"context"
	"sync"
	"time"
)

type DRStatusSnapshot struct {
	Overview   []NamespaceStatus
	Namespaces map[string][]ResourceStatusEntry
}

func (c *Comparator) Snapshot(ctx context.Context) (*DRStatusSnapshot, error) {
	resources := c.store.AllMeta()
	nsMap := make(map[string]*NamespaceStatus)
	nsDetail := make(map[string][]ResourceStatusEntry)

	for _, r := range resources {
		ns, ok := nsMap[r.Namespace]
		if !ok {
			ns = &NamespaceStatus{Namespace: r.Namespace}
			nsMap[r.Namespace] = ns
		}

		status, err := c.compareOne(ctx, r)
		if err != nil {
			status = StatusMissing
		}

		switch status {
		case StatusSynced:
			ns.Synced++
		case StatusDrifted:
			ns.Drifted++
		case StatusMissing:
			ns.Missing++
		}
		ns.Total++

		nsDetail[r.Namespace] = append(nsDetail[r.Namespace], ResourceStatusEntry{
			Namespace: r.Namespace,
			Kind:      r.Kind,
			Name:      r.Name,
			Status:    status,
			ScrapedAt: r.ScrapedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	var overview []NamespaceStatus
	for _, ns := range nsMap {
		overview = append(overview, *ns)
	}

	return &DRStatusSnapshot{
		Overview:   overview,
		Namespaces: nsDetail,
	}, nil
}

type DrStatusCache struct {
	comparator *Comparator
	mu         sync.RWMutex
	overview   []NamespaceStatus
	namespaces map[string][]ResourceStatusEntry
	comparing  bool
	lastAt     time.Time
	lastErr    string
}

func NewDrStatusCache(comparator *Comparator) *DrStatusCache {
	return &DrStatusCache{
		comparator: comparator,
		namespaces: make(map[string][]ResourceStatusEntry),
	}
}

type OverviewResponse struct {
	Namespaces     []NamespaceStatus `json:"namespaces"`
	Comparing      bool              `json:"comparing"`
	LastComparedAt *time.Time        `json:"lastComparedAt,omitempty"`
}

type NamespaceDetailResponse struct {
	Resources      []ResourceStatusEntry `json:"resources"`
	Comparing      bool                  `json:"comparing"`
	LastComparedAt *time.Time            `json:"lastComparedAt,omitempty"`
}

func (c *DrStatusCache) Overview() OverviewResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return OverviewResponse{
		Namespaces:     append([]NamespaceStatus{}, c.overview...),
		Comparing:      c.comparing,
		LastComparedAt: timeOrNil(c.lastAt),
	}
}

func (c *DrStatusCache) NamespaceDetail(ns string) NamespaceDetailResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	resources := c.namespaces[ns]
	if resources == nil {
		resources = []ResourceStatusEntry{}
	}
	return NamespaceDetailResponse{
		Resources:      append([]ResourceStatusEntry{}, resources...),
		Comparing:      c.comparing,
		LastComparedAt: timeOrNil(c.lastAt),
	}
}

func (c *DrStatusCache) ResourceStatus(namespace, kind, name string) (ResourceStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.namespaces[namespace] {
		if e.Kind == kind && e.Name == name {
			return e.Status, true
		}
	}
	return "", false
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (c *DrStatusCache) RefreshAsync() {
	c.mu.Lock()
	if c.comparing {
		c.mu.Unlock()
		return
	}
	c.comparing = true
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		c.refresh(ctx)
	}()
}

func (c *DrStatusCache) refresh(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.comparing = false
		c.mu.Unlock()
	}()

	if c.comparator == nil {
		c.mu.Lock()
		c.lastErr = "comparator not configured"
		c.mu.Unlock()
		return
	}

	snap, err := c.comparator.Snapshot(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastErr = err.Error()
		return
	}
	c.lastErr = ""
	c.overview = snap.Overview
	c.namespaces = snap.Namespaces
	c.lastAt = time.Now()
}

func (c *DrStatusCache) StartBackgroundRefresh(interval time.Duration) {
	c.RefreshAsync()
	go func() {
		ticker := time.NewTicker(interval)
		for range ticker.C {
			c.RefreshAsync()
		}
	}()
}
