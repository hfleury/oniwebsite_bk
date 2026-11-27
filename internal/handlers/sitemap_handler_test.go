package handlers

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/services"
)

// decodedURLSet mirrors sitemapURLSet for decoding: encoding/xml resolves the
// "xhtml:" prefix to its namespace on the way in, so the tag must name it.
type decodedURLSet struct {
	URLs []decodedURL `xml:"url"`
}

type decodedURL struct {
	Loc        string             `xml:"loc"`
	Alternates []sitemapAlternate `xml:"http://www.w3.org/1999/xhtml link"`
}

func serveSitemap(t *testing.T, translator core.TranslationService, forwardedProto string) (*httptest.ResponseRecorder, decodedURLSet) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	if forwardedProto != "" {
		req.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	w := httptest.NewRecorder()
	NewSitemapHandler(translator).ServeHTTP(w, req)

	var urlSet decodedURLSet
	if w.Code == http.StatusOK {
		if err := xml.Unmarshal(w.Body.Bytes(), &urlSet); err != nil {
			t.Fatalf("sitemap is not valid XML: %v\n%s", err, w.Body.String())
		}
	}
	return w, urlSet
}

func locs(urlSet decodedURLSet) map[string]decodedURL {
	byLoc := make(map[string]decodedURL, len(urlSet.URLs))
	for _, u := range urlSet.URLs {
		byLoc[u.Loc] = u
	}
	return byLoc
}

func TestSitemapHandler_ListsEveryPageInEveryLocale(t *testing.T) {
	fake := &fakeTranslationService{data: map[string]core.Translations{
		"en": {
			"case_studies_meta_title":          "Case Studies",
			"services_cloud_devops_meta_title": "Cloud",
			"case_studies_fixture_meta_title":  "Fixture",
		},
	}}

	w, urlSet := serveSitemap(t, fake, "https")

	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/xml") {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	if !strings.Contains(w.Body.String(), `xmlns:xhtml="http://www.w3.org/1999/xhtml"`) {
		t.Errorf("body missing xhtml namespace, got: %s", w.Body.String())
	}
	// 3 static pages + 1 service + 1 case study, each in 3 locales.
	if len(urlSet.URLs) != 15 {
		t.Errorf("got %d urls, want 15", len(urlSet.URLs))
	}

	byLoc := locs(urlSet)
	for _, want := range []string{
		"https://example.com/",
		"https://example.com/pt/",
		"https://example.com/sv/",
		"https://example.com/privacy",
		"https://example.com/sv/case-studies",
		"https://example.com/pt/services/cloud-devops",
		"https://example.com/case-studies/fixture",
	} {
		if _, ok := byLoc[want]; !ok {
			t.Errorf("sitemap missing <loc>%s</loc>", want)
		}
	}

	wantAlternates := []sitemapAlternate{
		{Rel: "alternate", Hreflang: "en", Href: "https://example.com/services/cloud-devops"},
		{Rel: "alternate", Hreflang: "pt", Href: "https://example.com/pt/services/cloud-devops"},
		{Rel: "alternate", Hreflang: "sv", Href: "https://example.com/sv/services/cloud-devops"},
		{Rel: "alternate", Hreflang: "x-default", Href: "https://example.com/services/cloud-devops"},
	}
	got := byLoc["https://example.com/sv/services/cloud-devops"].Alternates
	if len(got) != len(wantAlternates) {
		t.Fatalf("alternates = %v, want %v", got, wantAlternates)
	}
	for i := range wantAlternates {
		if got[i] != wantAlternates[i] {
			t.Errorf("alternate[%d] = %v, want %v", i, got[i], wantAlternates[i])
		}
	}
}

func TestSitemapHandler_TranslationErrorReturns500(t *testing.T) {
	fake := &fakeTranslationService{data: map[string]core.Translations{}}
	w, _ := serveSitemap(t, fake, "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestSitemapHandler_RealLocales guards that the sitemap lists every known
// page, and only pages the HTMLHandler serves with 200.
func TestSitemapHandler_RealLocales(t *testing.T) {
	svc := services.NewFileTranslationService("../../locales")
	if err := svc.LoadTranslations(); err != nil {
		t.Fatalf("LoadTranslations() returned unexpected error: %v", err)
	}
	_, urlSet := serveSitemap(t, svc, "")

	wantPages := len(sitemapStaticPages) + len(serviceIDs) + len(caseStudyIDs)
	if got, want := len(urlSet.URLs), wantPages*len(siteLocales); got != want {
		t.Errorf("got %d urls, want %d", got, want)
	}

	en, _ := svc.GetTranslations("en")
	for _, u := range urlSet.URLs {
		path := strings.TrimPrefix(u.Loc, "http://example.com")
		if !isPageRouteForTest(path) {
			t.Errorf("sitemap lists %s, which is not a page route", path)
		}
		if isUnknownDetailPath(path, en) {
			t.Errorf("sitemap lists %s, which the HTMLHandler serves as 404", path)
		}
	}
}

// isPageRouteForTest mirrors cmd/server's isPageRoute for the page kinds the
// sitemap lists.
func isPageRouteForTest(path string) bool {
	bare := stripLocalePrefix(path)
	if bare == "/" || bare == "/privacy" || bare == "/case-studies" {
		return true
	}
	_, isDetail := resolveDetailKeyPrefix(bare)
	return isDetail
}
