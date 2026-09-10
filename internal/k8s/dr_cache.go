package k8s

import (
	"context"
	"log"
	"sync"
	"time"
)

type DRStatusSnapshot struct {
	Overview   []NamespaceStatus
	Namespaces map[string][]ResourceStatusEntry
}

type DrStatusCache struct {
	comparator *Comparator
	busy       func() bool
	mu         sync.RWMutex
	overview   []NamespaceStatus
	namespaces map[string][]ResourceStatusEntry
	comparing  bool
	queued     bool
	lastAt     time.Time
	lastErr    string
}

func NewDrStatusCache(comparator *Comparator) *DrStatusCache {
	c := &DrStatusCache{
		comparator: comparator,
		namespaces: make(map[string][]ResourceStatusEntry),
	}
	c.HydrateFromStore()
	return c
}

func (c *DrStatusCache) HydrateFromStore() {
	if c.comparator == nil {
		return
	}
	snap := c.comparator.Inventory()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.overview = snap.Overview
	c.namespaces = snap.Namespaces
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

func (c *DrStatusCache) ResourceStatus(namespace, kind, name string) (ResourceStatus, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.namespaces[namespace] {
		if e.Kind == kind && e.Name == name {
			return e.Status, e.Drift, true
		}
	}
	return "", "", false
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (c *DrStatusCache) SetBusyCheck(fn func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = fn
}

func (c *DrStatusCache) RefreshAsync() {
	c.kick(true)
}

func (c *DrStatusCache) refreshIfIdle() {
	c.kick(false)
}

func (c *DrStatusCache) kick(queue bool) {
	c.mu.Lock()
	if c.comparing {
		if queue {
			c.queued = true
		}
		c.mu.Unlock()
		return
	}
	if c.busy != nil && c.busy() {
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
	started := time.Now()
	var queued bool
	defer func() {
		c.mu.Lock()
		queued = c.queued
		c.queued = false
		c.comparing = false
		c.mu.Unlock()
		if queued {
			c.RefreshAsync()
		}
	}()

	if c.comparator == nil {
		c.mu.Lock()
		c.lastErr = "comparator not configured"
		c.mu.Unlock()
		return
	}

	snap, err := c.comparator.Snapshot(ctx)
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.mu.Unlock()
		log.Printf("dr compare: %v", err)
		return
	}
	if err := c.comparator.PersistSnapshot(snap); err != nil {
		log.Printf("persist dr statuses: %v", err)
	}
	c.mu.Lock()
	c.lastErr = ""
	c.overview = snap.Overview
	c.namespaces = snap.Namespaces
	c.lastAt = time.Now()
	c.mu.Unlock()
	log.Printf("dr compare: finished in %s", time.Since(started).Round(time.Millisecond))
}

func (c *DrStatusCache) StartBackgroundRefresh(interval time.Duration) {
	c.RefreshAsync()
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		for range ticker.C {
			c.refreshIfIdle()
		}
	}()
}
