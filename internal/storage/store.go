package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ResourceWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResourceMeta struct {
	Namespace       string            `json:"namespace"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	APIVersion      string            `json:"apiVersion"`
	Resource        string            `json:"resource"` // plural API resource name
	ScrapedAt       time.Time         `json:"scrapedAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	Edited          bool              `json:"edited,omitempty"`
	EditedAt        time.Time         `json:"editedAt,omitempty"`
	Status          string            `json:"status,omitempty"`
	Drift           string            `json:"drift,omitempty"`
	Warnings        []ResourceWarning `json:"warnings,omitempty"`
	WarningsChecked bool              `json:"warningsChecked,omitempty"`
}

type StatusUpdate struct {
	Namespace string
	Kind      string
	Name      string
	Status    string
	Drift     string
}

type WarningUpdate struct {
	Namespace string
	Kind      string
	Name      string
	Warnings  []ResourceWarning
}

type Index struct {
	Resources   []ResourceMeta `json:"resources"`
	ScrapeStats ScrapeStats    `json:"scrapeStats"`
}

type ScrapeStats struct {
	LastScrapeAt   time.Time `json:"lastScrapeAt"`
	NamespaceCount int       `json:"namespaceCount"`
	ResourceCount  int       `json:"resourceCount"`
	Namespaces     []string  `json:"namespaces"`
}

const indexFlushEvery = 25

type Store struct {
	basePath string
	enc      encryptFunc
	mu       sync.RWMutex
	index    Index
	dirty    int
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
	if err := s.reconcileDisk(); err != nil {
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

func (s *Store) reconcileDisk() error {
	root := filepath.Join(s.basePath, "manifests")
	known := make(map[string]struct{}, len(s.index.Resources))
	for _, r := range s.index.Resources {
		known[r.Namespace+"/"+r.Kind+"/"+r.Name] = struct{}{}
	}

	var added int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".enc") {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".edit.enc") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 {
			return nil
		}
		ns, kind, name := parts[0], parts[1], strings.TrimSuffix(parts[2], ".enc")
		key := ns + "/" + kind + "/" + name
		if _, ok := known[key]; ok {
			return nil
		}
		mtime := time.Now()
		if info, err := d.Info(); err == nil {
			mtime = info.ModTime()
		}
		s.index.Resources = append(s.index.Resources, ResourceMeta{
			Namespace: ns,
			Kind:      kind,
			Name:      name,
			ScrapedAt: mtime,
			UpdatedAt: mtime,
		})
		known[key] = struct{}{}
		added++
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if added == 0 {
		return nil
	}
	return s.saveIndex()
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

func editPath(base, namespace, kind, name string) string {
	safeKind := strings.ReplaceAll(kind, "/", "_")
	return filepath.Join(base, "manifests", namespace, safeKind, name+".edit.enc")
}

func (s *Store) writeEncrypted(path string, yamlContent []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	enc, err := s.enc.Encrypt(yamlContent)
	if err != nil {
		return err
	}
	return os.WriteFile(path, enc, 0600)
}

func (s *Store) readEncrypted(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return s.enc.Decrypt(data)
}

func yamlEqual(a, b []byte) bool {
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}

func (s *Store) Save(meta ResourceMeta, yamlContent []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	meta.UpdatedAt = now
	if meta.ScrapedAt.IsZero() {
		meta.ScrapedAt = now
	}

	existing, ok := s.findMeta(meta.Namespace, meta.Kind, meta.Name)
	if ok {
		meta.Edited = existing.Edited
		meta.EditedAt = existing.EditedAt
		if meta.Status == "" {
			meta.Status = existing.Status
			meta.Drift = existing.Drift
		}
	}

	path := manifestPath(s.basePath, meta.Namespace, meta.Kind, meta.Name)
	if err := s.writeEncrypted(path, yamlContent); err != nil {
		return err
	}

	ep := editPath(s.basePath, meta.Namespace, meta.Kind, meta.Name)
	edited, err := s.readEncrypted(ep)
	if err == nil {
		if yamlEqual(edited, yamlContent) {
			_ = os.Remove(ep)
			meta.Edited = false
			meta.EditedAt = time.Time{}
		} else {
			meta.Edited = true
			meta.WarningsChecked = false
			meta.Warnings = nil
		}
	} else if os.IsNotExist(err) {
		meta.Edited = false
		meta.EditedAt = time.Time{}
	} else {
		return err
	}

	s.upsertMeta(meta)
	s.dirty++
	if s.dirty >= indexFlushEvery {
		return s.saveIndexLocked()
	}
	return nil
}

func (s *Store) SaveEdit(namespace, kind, name string, yamlContent []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scraped, err := s.readEncrypted(manifestPath(s.basePath, namespace, kind, name))
	if err != nil {
		return err
	}
	meta, ok := s.findMeta(namespace, kind, name)
	if !ok {
		return fmt.Errorf("resource not found")
	}

	ep := editPath(s.basePath, namespace, kind, name)
	now := time.Now()
	if yamlEqual(scraped, yamlContent) {
		if err := os.Remove(ep); err != nil && !os.IsNotExist(err) {
			return err
		}
		meta.Edited = false
		meta.EditedAt = time.Time{}
		meta.UpdatedAt = now
		s.upsertMeta(meta)
		return s.saveIndexLocked()
	}

	if err := s.writeEncrypted(ep, yamlContent); err != nil {
		return err
	}
	meta.Edited = true
	meta.EditedAt = now
	meta.UpdatedAt = now
	s.upsertMeta(meta)
	return s.saveIndexLocked()
}

func (s *Store) ClearEdit(namespace, kind, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ep := editPath(s.basePath, namespace, kind, name)
	if err := os.Remove(ep); err != nil && !os.IsNotExist(err) {
		return err
	}
	meta, ok := s.findMeta(namespace, kind, name)
	if !ok {
		return nil
	}
	meta.Edited = false
	meta.EditedAt = time.Time{}
	meta.UpdatedAt = time.Now()
	meta.WarningsChecked = false
	meta.Warnings = nil
	s.upsertMeta(meta)
	return s.saveIndexLocked()
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty == 0 {
		return nil
	}
	return s.saveIndexLocked()
}

func (s *Store) saveIndexLocked() error {
	if err := s.saveIndex(); err != nil {
		return err
	}
	s.dirty = 0
	return nil
}

func (s *Store) upsertMeta(meta ResourceMeta) {
	for i, r := range s.index.Resources {
		if r.Namespace == meta.Namespace && r.Kind == meta.Kind && r.Name == meta.Name {
			if meta.Status == "" {
				meta.Status = r.Status
				meta.Drift = r.Drift
			}
			s.index.Resources[i] = meta
			return
		}
	}
	s.index.Resources = append(s.index.Resources, meta)
}

func (s *Store) findMeta(namespace, kind, name string) (ResourceMeta, bool) {
	for _, r := range s.index.Resources {
		if r.Namespace == namespace && r.Kind == kind && r.Name == name {
			return r, true
		}
	}
	return ResourceMeta{}, false
}

func (s *Store) ApplyStatuses(updates []StatusUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := make(map[string]int, len(s.index.Resources))
	for i, r := range s.index.Resources {
		idx[r.Namespace+"/"+r.Kind+"/"+r.Name] = i
	}

	changed := false
	for _, u := range updates {
		i, ok := idx[u.Namespace+"/"+u.Kind+"/"+u.Name]
		if !ok {
			continue
		}
		if s.index.Resources[i].Status == u.Status && s.index.Resources[i].Drift == u.Drift {
			continue
		}
		s.index.Resources[i].Status = u.Status
		s.index.Resources[i].Drift = u.Drift
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveIndexLocked()
}

func warningsEqual(a, b []ResourceWarning) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].Message != b[i].Message {
			return false
		}
	}
	return true
}

func (s *Store) ApplyWarnings(updates []WarningUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := make(map[string]int, len(s.index.Resources))
	for i, r := range s.index.Resources {
		idx[r.Namespace+"/"+r.Kind+"/"+r.Name] = i
	}

	changed := false
	for _, u := range updates {
		i, ok := idx[u.Namespace+"/"+u.Kind+"/"+u.Name]
		if !ok {
			continue
		}
		if s.index.Resources[i].WarningsChecked && warningsEqual(s.index.Resources[i].Warnings, u.Warnings) {
			continue
		}
		s.index.Resources[i].Warnings = u.Warnings
		s.index.Resources[i].WarningsChecked = true
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveIndexLocked()
}

func (s *Store) Get(namespace, kind, name string) ([]byte, ResourceMeta, error) {
	files, err := s.GetFiles(namespace, kind, name)
	if err != nil {
		return nil, ResourceMeta{}, err
	}
	return files.YAML, files.Meta, nil
}

type ResourceFiles struct {
	YAML    []byte
	Scraped []byte
	Edited  []byte
	HasEdit bool
	Meta    ResourceMeta
}

func (s *Store) GetFiles(namespace, kind, name string) (ResourceFiles, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scraped, err := s.readEncrypted(manifestPath(s.basePath, namespace, kind, name))
	if err != nil {
		return ResourceFiles{}, err
	}
	meta, _ := s.findMeta(namespace, kind, name)
	out := ResourceFiles{YAML: scraped, Scraped: scraped, Meta: meta}
	edited, err := s.readEncrypted(editPath(s.basePath, namespace, kind, name))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return ResourceFiles{}, err
	}
	out.Edited = edited
	out.HasEdit = true
	out.YAML = edited
	out.Meta.Edited = true
	return out, nil
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
	if err := os.Remove(editPath(s.basePath, namespace, kind, name)); err != nil && !os.IsNotExist(err) {
		return err
	}

	var filtered []ResourceMeta
	found := false
	for _, r := range s.index.Resources {
		if r.Namespace == namespace && r.Kind == kind && r.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !found {
		return nil
	}
	s.index.Resources = filtered
	return s.saveIndexLocked()
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
	return s.saveIndexLocked()
}

func (s *Store) ScrapeStats() ScrapeStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.ScrapeStats
}

func (s *Store) LiveStats() ScrapeStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, r := range s.index.Resources {
		if r.Namespace != "" {
			seen[r.Namespace] = struct{}{}
		}
	}
	namespaces := make([]string, 0, len(seen))
	for ns := range seen {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	stats := s.index.ScrapeStats
	stats.NamespaceCount = len(namespaces)
	stats.ResourceCount = len(s.index.Resources)
	stats.Namespaces = namespaces
	return stats
}

func (s *Store) AllMeta() []ResourceMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ResourceMeta{}, s.index.Resources...)
}
