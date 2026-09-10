package storage

import (
	"bytes"
	"testing"

	"github.com/jamie/drkate/internal/crypto"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(t.TempDir(), enc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLiveStatsCountsUnflushedSaves(t *testing.T) {
	s := testStore(t)
	if err := s.Save(ResourceMeta{Namespace: "alpha", Kind: "ConfigMap", Name: "a"}, []byte("k: a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ResourceMeta{Namespace: "beta", Kind: "Secret", Name: "b"}, []byte("k: b")); err != nil {
		t.Fatal(err)
	}

	live := s.LiveStats()
	if live.ResourceCount != 2 {
		t.Fatalf("resourceCount=%d, want 2", live.ResourceCount)
	}
	if live.NamespaceCount != 2 {
		t.Fatalf("namespaceCount=%d, want 2", live.NamespaceCount)
	}
	if live.Namespaces[0] != "alpha" || live.Namespaces[1] != "beta" {
		t.Fatalf("namespaces=%v", live.Namespaces)
	}
}

func TestReopenRecoversUnflushedSaves(t *testing.T) {
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := NewStore(dir, enc)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ResourceMeta{Namespace: "ns", Kind: "ConfigMap", Name: "a"}, []byte("k: a")); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(dir, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.List("ns", "")); got != 1 {
		t.Fatalf("expected disk recovery of unflushed save, got %d", got)
	}
}

func TestApplyWarningsPersists(t *testing.T) {
	s := testStore(t)
	if err := s.Save(ResourceMeta{Namespace: "ns", Kind: "PersistentVolumeClaim", Name: "data"}, []byte("kind: PersistentVolumeClaim")); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyWarnings([]WarningUpdate{{
		Namespace: "ns",
		Kind:      "PersistentVolumeClaim",
		Name:      "data",
		Warnings:  []ResourceWarning{{Code: "storage", Message: "PVC uses longhorn"}},
	}}); err != nil {
		t.Fatal(err)
	}
	_, meta, err := s.Get("ns", "PersistentVolumeClaim", "data")
	if err != nil {
		t.Fatal(err)
	}
	if !meta.WarningsChecked || len(meta.Warnings) != 1 || meta.Warnings[0].Code != "storage" {
		t.Fatalf("meta=%#v", meta)
	}
}

func TestSaveEditSurvivesScrape(t *testing.T) {
	s := testStore(t)
	meta := ResourceMeta{Namespace: "ns", Kind: "ConfigMap", Name: "app"}
	if err := s.Save(meta, []byte("data:\n  k: source\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdit("ns", "ConfigMap", "app", []byte("data:\n  k: edited\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(meta, []byte("data:\n  k: source2\n")); err != nil {
		t.Fatal(err)
	}
	files, err := s.GetFiles("ns", "ConfigMap", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !files.HasEdit || string(files.YAML) != "data:\n  k: edited\n" {
		t.Fatalf("effective=%q hasEdit=%v", files.YAML, files.HasEdit)
	}
	if string(files.Scraped) != "data:\n  k: source2\n" {
		t.Fatalf("scraped=%q", files.Scraped)
	}
}

func TestSaveEditMatchingScrapedClearsOverlay(t *testing.T) {
	s := testStore(t)
	meta := ResourceMeta{Namespace: "ns", Kind: "ConfigMap", Name: "app"}
	if err := s.Save(meta, []byte("data:\n  k: source\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdit("ns", "ConfigMap", "app", []byte("data:\n  k: edited\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdit("ns", "ConfigMap", "app", []byte("data:\n  k: source\n")); err != nil {
		t.Fatal(err)
	}
	files, err := s.GetFiles("ns", "ConfigMap", "app")
	if err != nil {
		t.Fatal(err)
	}
	if files.HasEdit {
		t.Fatal("expected overlay to be cleared")
	}
}

func TestReconcileIgnoresEditFiles(t *testing.T) {
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := NewStore(dir, enc)
	if err != nil {
		t.Fatal(err)
	}
	meta := ResourceMeta{Namespace: "ns", Kind: "ConfigMap", Name: "app"}
	if err := s.Save(meta, []byte("k: a")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdit("ns", "ConfigMap", "app", []byte("k: b")); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(dir, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.List("ns", "")); got != 1 {
		t.Fatalf("got %d resources", got)
	}
}
