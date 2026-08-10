package services

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write fixture %s: %v", name, err)
	}
}

func TestLoadTranslations_CacheHit(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "en.json", `{"meta_title": "English Title"}`)
	writeFixture(t, dir, "pt.json", `{"meta_title": "Titulo em Portugues"}`)

	svc := NewFileTranslationService(dir)
	if err := svc.LoadTranslations(); err != nil {
		t.Fatalf("LoadTranslations() returned unexpected error: %v", err)
	}

	translations, err := svc.GetTranslations("en")
	if err != nil {
		t.Fatalf("GetTranslations(\"en\") returned unexpected error: %v", err)
	}
	if got, want := translations["meta_title"], "English Title"; got != want {
		t.Errorf("meta_title = %v, want %v", got, want)
	}
}

func TestLoadTranslations_CacheMiss(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "en.json", `{"meta_title": "English Title"}`)
	writeFixture(t, dir, "pt.json", `{"meta_title": "Titulo em Portugues"}`)

	svc := NewFileTranslationService(dir)
	if err := svc.LoadTranslations(); err != nil {
		t.Fatalf("LoadTranslations() returned unexpected error: %v", err)
	}

	translations, err := svc.GetTranslations("fr")
	if err == nil {
		t.Fatalf("GetTranslations(\"fr\") expected an error, got nil")
	}
	if translations != nil {
		t.Errorf("GetTranslations(\"fr\") translations = %v, want nil", translations)
	}
}

func TestLoadTranslations_BadDir(t *testing.T) {
	svc := NewFileTranslationService("/nonexistent/path")

	if err := svc.LoadTranslations(); err == nil {
		t.Fatal("LoadTranslations() expected an error for a nonexistent directory, got nil")
	}
}

func TestLoadTranslations_SkipsNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "en.json", `{"meta_title": "English Title"}`)
	writeFixture(t, dir, "notes.txt", `not a translation file`)

	svc := NewFileTranslationService(dir)
	if err := svc.LoadTranslations(); err != nil {
		t.Fatalf("LoadTranslations() returned unexpected error with a non-JSON file present: %v", err)
	}

	translations, err := svc.GetTranslations("en")
	if err != nil {
		t.Fatalf("GetTranslations(\"en\") returned unexpected error: %v", err)
	}
	if got, want := translations["meta_title"], "English Title"; got != want {
		t.Errorf("meta_title = %v, want %v", got, want)
	}
}
