package handlers

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/observability"
)

// sitemapStaticPages are the page routes that aren't id-keyed detail pages.
var sitemapStaticPages = []string{"/", "/case-studies", "/privacy"}

type sitemapURLSet struct {
	XMLName    xml.Name     `xml:"urlset"`
	Xmlns      string       `xml:"xmlns,attr"`
	XmlnsXHTML string       `xml:"xmlns:xhtml,attr"`
	URLs       []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string             `xml:"loc"`
	Alternates []sitemapAlternate `xml:"xhtml:link"`
}

type sitemapAlternate struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// SitemapHandler serves /sitemap.xml: every page route in every locale, each
// with hreflang alternates shaped like the HTMLHandler's hreflang links.
type SitemapHandler struct {
	Translator core.TranslationService
}

func NewSitemapHandler(t core.TranslationService) *SitemapHandler {
	return &SitemapHandler{Translator: t}
}

func (h *SitemapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every locale has the same keys (TestLocalesKeyParity), so en lists the
	// detail ids for all of them.
	translations, err := h.Translator.GetTranslations("en")
	if err != nil {
		slog.ErrorContext(r.Context(), "sitemap translation load failed", observability.TraceIDAttr(r.Context()), slog.Any("error", err))
		observability.CaptureException(r.Context(), err)
		http.Error(w, "sitemap unavailable", http.StatusInternalServerError)
		return
	}

	origin := fmt.Sprintf("%s://%s", resolveScheme(r), r.Host)
	urlSet := sitemapURLSet{
		Xmlns:      "http://www.sitemaps.org/schemas/sitemap/0.9",
		XmlnsXHTML: "http://www.w3.org/1999/xhtml",
	}
	for _, bare := range sitemapPages(translations) {
		alternates := localeAlternates(origin, bare)
		for _, locale := range siteLocales {
			urlSet.URLs = append(urlSet.URLs, sitemapURL{
				Loc:        origin + withLocalePrefix(bare, locale),
				Alternates: alternates,
			})
		}
	}

	body, err := xml.Marshal(urlSet)
	if err != nil {
		slog.ErrorContext(r.Context(), "sitemap marshal failed", observability.TraceIDAttr(r.Context()), slog.Any("error", err))
		observability.CaptureException(r.Context(), err)
		http.Error(w, "sitemap unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	w.Write(body)
}

// sitemapPages returns the locale-less path of every page route: the static
// pages, then each detail section's ids.
func sitemapPages(translations core.Translations) []string {
	pages := append([]string(nil), sitemapStaticPages...)
	for _, section := range detailSections {
		for _, id := range listDetailIDs(translations, section.keySection) {
			pages = append(pages, section.urlPrefix+id)
		}
	}
	return pages
}

// localeAlternates returns bare's hreflang alternates: one per locale plus
// x-default pointing at the en URL.
func localeAlternates(origin, bare string) []sitemapAlternate {
	alternates := make([]sitemapAlternate, 0, len(siteLocales)+1)
	for _, locale := range siteLocales {
		alternates = append(alternates, sitemapAlternate{Rel: "alternate", Hreflang: locale, Href: origin + withLocalePrefix(bare, locale)})
	}
	return append(alternates, sitemapAlternate{Rel: "alternate", Hreflang: "x-default", Href: origin + withLocalePrefix(bare, "en")})
}
