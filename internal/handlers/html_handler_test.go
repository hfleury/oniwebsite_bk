package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/middleware"
)

const fixtureIndexHTML = `<html lang="en">
<head>
<title>placeholder</title>
</head>
<body></body>
</html>`

const fixtureIndexHTMLNoLangAttr = `<html>
<head>
<title>placeholder</title>
</head>
<body></body>
</html>`

type fakeTranslationService struct {
	data map[string]core.Translations
}

func (f *fakeTranslationService) LoadTranslations() error {
	return nil
}

func (f *fakeTranslationService) GetTranslations(lang string) (core.Translations, error) {
	translations, ok := f.data[lang]
	if !ok {
		return nil, fmt.Errorf("translations not found for language: %s", lang)
	}
	return translations, nil
}

func writeIndexHTML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write fixture index.html: %v", err)
	}
	return dir
}

func requestWithLang(lang string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if lang != "" {
		ctx := context.WithValue(req.Context(), middleware.CtxLanguageKey, lang)
		req = req.WithContext(ctx)
	}
	return req
}

func TestHTMLHandler_ServeHTTP_InjectsPerLanguage(t *testing.T) {
	for _, lang := range []string{"en", "pt", "sv"} {
		t.Run(lang, func(t *testing.T) {
			distDir := writeIndexHTML(t, fixtureIndexHTML)
			fake := &fakeTranslationService{data: map[string]core.Translations{
				lang: {"meta_title": "Title for " + lang},
			}}
			handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

			req := requestWithLang(lang)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			body := w.Body.String()

			wantLangTag := fmt.Sprintf(`<html lang="%s">`, lang)
			if !strings.Contains(body, wantLangTag) {
				t.Errorf("body missing %q, got: %s", wantLangTag, body)
			}

			expectedJSON, err := json.Marshal(fake.data[lang])
			if err != nil {
				t.Fatalf("failed to marshal expected translations: %v", err)
			}
			wantScript := fmt.Sprintf("<script>window.__INITIAL_STATE__ = %s;</script>", string(expectedJSON))
			if !strings.Contains(body, wantScript) {
				t.Errorf("body missing injected state script, got: %s", body)
			}

			wantTitle := fmt.Sprintf("<title>Title for %s</title>", lang)
			if !strings.Contains(body, wantTitle) {
				t.Errorf("body missing %q, got: %s", wantTitle, body)
			}
		})
	}
}

func TestHTMLHandler_ServeHTTP_NoLanguageInContextDefaultsEnglish(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {"meta_title": "English Title"},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLang("")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("body missing english lang tag, got: %s", body)
	}
	if !strings.Contains(body, "<title>English Title</title>") {
		t.Errorf("body missing english title, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_FallsBackToEnglishOnTranslationError(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {"meta_title": "English Title"},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLang("fr")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>English Title</title>") {
		t.Errorf("expected fallback to english title, got: %s", body)
	}

	expectedJSON, err := json.Marshal(fake.data["en"])
	if err != nil {
		t.Fatalf("failed to marshal expected translations: %v", err)
	}
	wantScript := fmt.Sprintf("<script>window.__INITIAL_STATE__ = %s;</script>", string(expectedJSON))
	if !strings.Contains(body, wantScript) {
		t.Errorf("body missing injected fallback state script, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_NoMetaTitleLeavesPlaceholderUnchanged(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {"some_other_key": "value"},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLang("en")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>placeholder</title>") {
		t.Errorf("expected placeholder title to remain unchanged, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_BareHTMLTagFallbackInjectsLang(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTMLNoLangAttr)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"pt": {"meta_title": "Titulo em Portugues"},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLang("pt")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `<html lang="pt">`) {
		t.Errorf("expected bare <html> fallback to inject lang attribute, got: %s", body)
	}
}
