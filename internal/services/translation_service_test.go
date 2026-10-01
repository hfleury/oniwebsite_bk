package services

import (
	"os"
	"path/filepath"
	"sort"
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

// TestLocalesKeyParity guards the real locales/ directory: every locale file
// must parse (the server exits at startup otherwise) and pt must carry exactly
// the en key set (a missing key renders as the raw key on /pt/).
func TestLocalesKeyParity(t *testing.T) {
	svc := NewFileTranslationService("../../locales")
	if err := svc.LoadTranslations(); err != nil {
		t.Fatalf("LoadTranslations() returned unexpected error: %v", err)
	}

	en, err := svc.GetTranslations("en")
	if err != nil {
		t.Fatalf("GetTranslations(\"en\") returned unexpected error: %v", err)
	}
	pt, err := svc.GetTranslations("pt")
	if err != nil {
		t.Fatalf("GetTranslations(\"pt\") returned unexpected error: %v", err)
	}

	if missing := keysNotIn(en, pt); len(missing) > 0 {
		t.Errorf("pt.json is missing keys present in en.json: %v", missing)
	}
	if extra := keysNotIn(pt, en); len(extra) > 0 {
		t.Errorf("pt.json has keys absent from en.json: %v", extra)
	}
}

// keysNotIn returns the sorted keys of from that are absent from other.
func keysNotIn(from, other map[string]interface{}) []string {
	var keys []string
	for key := range from {
		if _, ok := other[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
