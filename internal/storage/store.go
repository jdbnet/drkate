package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ResourceMeta struct {
	Namespace  string    `json:"namespace"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	APIVersion string    `json:"apiVersion"`
	Resource   string    `json:"resource"` // plural API resource name
	ScrapedAt  time.Time `json:"scrapedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Index struct {
	Resources []ResourceMeta `json:"resources"`
	ScrapeStats ScrapeStats    `json:"scrapeStats"`
}

type ScrapeStats struct {
	LastScrapeAt time.Time `json:"lastScrapeAt"`
	NamespaceCount int     `json:"namespaceCount"`
	ResourceCount  int     `json:"resourceCount"`
	Namespaces     []string `json:"namespaces"`
}

type Store struct {
	basePath string
	enc      encryptFunc
	mu       sync.RWMutex
	index    Index
}

type encryptFunc interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

func NewStore(basePath string, enc encryptFunc) (*Store, error) {
	s := &Store{
		basePath: basePath,
		enc:      enc,
	}
	if err := os.MkdirAll(filepath.Join(basePath, "manifests"), 0755); err != nil {
		return nil, err
	}
	if err := s.loadIndex(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadIndex() error {
	path := filepath.Join(s.basePath, "index.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	plain, err := s.enc.Decrypt(data)
	if err != nil {
		return fmt.Errorf("decrypt index: %w", err)
	}
	return json.Unmarshal(plain, &s.index)
}

func (s *Store) saveIndex() error {
	data, err := json.Marshal(s.index)
	if err != nil {
		return err
	}
	enc, err := s.enc.Encrypt(data)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.basePath, "index.enc"), enc, 0600)
}

func manifestPath(base, namespace, kind, name string) string {
	safeKind := strings.ReplaceAll(kind, "/", "_")
	return filepath.Join(base, "manifests", namespace, safeKind, name+".enc")
}

func (s *Store) Save(meta ResourceMeta, yamlContent []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	meta.UpdatedAt = now
	if meta.ScrapedAt.IsZero() {
		meta.ScrapedAt = now
	}

	path := manifestPath(s.basePath, meta.Namespace, meta.Kind, meta.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	enc, err := s.enc.Encrypt(yamlContent)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, enc, 0600); err != nil {
		return err
	}

	s.upsertMeta(meta)
	return s.saveIndex()
}

func (s *Store) upsertMeta(meta ResourceMeta) {
	for i, r := range s.index.Resources {
		if r.Namespace == meta.Namespace && r.Kind == meta.Kind && r.Name == meta.Name {
			s.index.Resources[i] = meta
			return
		}
	}
	s.index.Resources = append(s.index.Resources, meta)
}

func (s *Store) Get(namespace, kind, name string) ([]byte, ResourceMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := manifestPath(s.basePath, namespace, kind, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ResourceMeta{}, err
	}
	plain, err := s.enc.Decrypt(data)
	if err != nil {
		return nil, ResourceMeta{}, err
	}

	var meta ResourceMeta
	for _, r := range s.index.Resources {
		if r.Namespace == namespace && r.Kind == kind && r.Name == name {
			meta = r
			break
		}
	}
	return plain, meta, nil
}

func (s *Store) List(namespace, kind string) []ResourceMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []ResourceMeta
	for _, r := range s.index.Resources {
		if namespace != "" && r.Namespace != namespace {
			continue
		}
		if kind != "" && r.Kind != kind {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (s *Store) Delete(namespace, kind, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := manifestPath(s.basePath, namespace, kind, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	var filtered []ResourceMeta
	for _, r := range s.index.Resources {
		if r.Namespace == namespace && r.Kind == kind && r.Name == name {
			continue
		}
		filtered = append(filtered, r)
	}
	s.index.Resources = filtered
	return s.saveIndex()
}

func (s *Store) UpdateScrapeStats(namespaces []string, resourceCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index.ScrapeStats = ScrapeStats{
		LastScrapeAt:   time.Now(),
		NamespaceCount: len(namespaces),
		ResourceCount:  resourceCount,
		Namespaces:     namespaces,
	}
	return s.saveIndex()
}

func (s *Store) ScrapeStats() ScrapeStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.ScrapeStats
}

func (s *Store) AllMeta() []ResourceMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ResourceMeta{}, s.index.Resources...)
}
