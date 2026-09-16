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

func requestWithLangAndPath(lang, path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
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

func TestHTMLHandler_ServeHTTP_ServiceSlugMatchesSpecificMetaTitle(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {
			"meta_title": "Generic Title",
			"services_enterprise_software_meta_title": "Enterprise Software Development | Oni Web Officer",
		},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLangAndPath("en", "/services/enterprise-software")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>Enterprise Software Development | Oni Web Officer</title>") {
		t.Errorf("expected slug-specific title, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_ServiceSlugFallsBackToGenericMetaTitle(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {"meta_title": "Generic Title"},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLangAndPath("en", "/services/unknown-slug")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>Generic Title</title>") {
		t.Errorf("expected fallback to generic title, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_NonServicePathUsesGenericMetaTitle(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {
			"meta_title": "Generic Title",
			"services_enterprise_software_meta_title": "Enterprise Software Development | Oni Web Officer",
		},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLangAndPath("en", "/")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>Generic Title</title>") {
		t.Errorf("expected generic title on non-service path, got: %s", body)
	}
}

func TestHTMLHandler_ServeHTTP_LocalePrefixedServiceSlugResolves(t *testing.T) {
	distDir := writeIndexHTML(t, fixtureIndexHTML)
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"pt": {
			"meta_title": "Titulo Generico",
			"services_enterprise_software_meta_title": "Desenvolvimento de Software Empresarial",
		},
	}}
	handler := &HTMLHandler{Translator: fake, IsDev: false, DistDir: distDir}

	req := requestWithLangAndPath("pt", "/pt/services/enterprise-software")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "<title>Desenvolvimento de Software Empresarial</title>") {
		t.Errorf("expected slug-specific title for locale-prefixed path, got: %s", body)
	}
}

func TestResolveMeta(t *testing.T) {
	tests := []struct {
		name         string
		translations core.Translations
		slug         string
		field        string
		wantValue    string
		wantOK       bool
	}{
		{
			name: "title: slug-specific key present",
			translations: core.Translations{
				"meta_title": "Generic Title",
				"services_enterprise_software_meta_title": "Specific Title",
			},
			slug:      "enterprise-software",
			field:     "title",
			wantValue: "Specific Title",
			wantOK:    true,
		},
		{
			name:         "title: slug-specific key missing falls back to generic",
			translations: core.Translations{"meta_title": "Generic Title"},
			slug:         "enterprise-software",
			field:        "title",
			wantValue:    "Generic Title",
			wantOK:       true,
		},
		{
			name:         "title: empty slug uses generic",
			translations: core.Translations{"meta_title": "Generic Title"},
			slug:         "",
			field:        "title",
			wantValue:    "Generic Title",
			wantOK:       true,
		},
		{
			name:         "title: no keys at all returns not-ok",
			translations: core.Translations{},
			slug:         "enterprise-software",
			field:        "title",
			wantValue:    "",
			wantOK:       false,
		},
		{
			name: "description: slug-specific key present",
			translations: core.Translations{
				"meta_description": "Generic Description",
				"services_enterprise_software_meta_description": "Specific Description",
			},
			slug:      "enterprise-software",
			field:     "description",
			wantValue: "Specific Description",
			wantOK:    true,
		},
		{
			name:         "description: slug-specific key missing falls back to generic",
			translations: core.Translations{"meta_description": "Generic Description"},
			slug:         "enterprise-software",
			field:        "description",
			wantValue:    "Generic Description",
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := resolveMeta(tt.translations, tt.slug, tt.field)
			if gotValue != tt.wantValue || gotOK != tt.wantOK {
				t.Errorf("resolveMeta(%v, %q, %q) = (%q, %v), want (%q, %v)",
					tt.translations, tt.slug, tt.field, gotValue, gotOK, tt.wantValue, tt.wantOK)
			}
		})
	}
}
