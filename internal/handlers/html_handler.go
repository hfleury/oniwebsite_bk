package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/middleware"
	"oniwebsite_bk/internal/observability"
)

type HTMLHandler struct {
	Translator core.TranslationService
	IsDev      bool
	DevTarget  string
	DistDir    string
}

func NewHTMLHandler(t core.TranslationService, isDev bool, distDir string) *HTMLHandler {
	return &HTMLHandler{
		Translator: t,
		IsDev:      isDev,
		DevTarget:  "http://localhost:5173", // Standard Vite port
		DistDir:    distDir,
	}
}

// stripLocalePrefix removes a leading "/pt" or "/sv" locale prefix from path,
// mirroring the prefix check in middleware.LanguageDetectorMiddleware.
func stripLocalePrefix(path string) string {
	if strings.HasPrefix(path, "/pt") {
		return strings.TrimPrefix(path, "/pt")
	}
	if strings.HasPrefix(path, "/sv") {
		return strings.TrimPrefix(path, "/sv")
	}
	return path
}

// detailSection pairs the URL section of an id-keyed detail page with the
// locale key section its per-page keys live under.
type detailSection struct {
	urlPrefix  string
	keySection string
}

// detailSections lists every id-keyed detail page kind the site serves.
var detailSections = []detailSection{
	{urlPrefix: "/services/", keySection: "services"},
	{urlPrefix: "/case-studies/", keySection: "case_studies"},
}

// extractDetailSlug returns the first path segment after urlPrefix in bare
// (a path with its locale prefix already stripped), and whether bare is
// under urlPrefix at all. The slug is "" for urlPrefix itself.
func extractDetailSlug(bare, urlPrefix string) (string, bool) {
	if !strings.HasPrefix(bare, urlPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(bare, urlPrefix)
	if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
		rest = rest[:slashIdx]
	}
	return rest, true
}

// resolveDetailKeyPrefix reports whether bare is a detail page and, when it
// names a slug, the key prefix its per-page keys use: "<keySection>_<slug>"
// with hyphens underscored, or "" for an empty slug.
func resolveDetailKeyPrefix(bare string) (keyPrefix string, isDetail bool) {
	for _, section := range detailSections {
		slug, ok := extractDetailSlug(bare, section.urlPrefix)
		if !ok {
			continue
		}
		if slug == "" {
			return "", true
		}
		return section.keySection + "_" + strings.ReplaceAll(slug, "-", "_"), true
	}
	return "", false
}

// listDetailIDs returns, sorted, every id that has a
// "<keySection>_<id>_meta_title" key in translations, with underscores turned
// back into the URL's hyphens. Like isUnknownDetailPath, it treats the
// locale's meta keys as the source of truth for which ids exist.
func listDetailIDs(translations core.Translations, keySection string) []string {
	prefix := keySection + "_"
	const suffix = "_meta_title"
	var ids []string
	for key := range translations {
		// The length check rejects the section's own "<keySection>_meta_title"
		// (e.g. the /case-studies index), where prefix and suffix overlap.
		if len(key) <= len(prefix)+len(suffix) || !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		id := key[len(prefix) : len(key)-len(suffix)]
		ids = append(ids, strings.ReplaceAll(id, "_", "-"))
	}
	sort.Strings(ids)
	return ids
}

// resolveScheme determines the request scheme, preferring the
// X-Forwarded-Proto header (set by a reverse proxy terminating TLS in front
// of this process) over r.TLS, since main.go only ever calls
// http.ListenAndServe and never terminates TLS itself.
func resolveScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// siteLocales lists every locale the site serves, in hreflang order.
var siteLocales = []string{"en", "pt", "sv"}

// withLocalePrefix returns bare unchanged for the "en" locale, otherwise
// prefixes it with "/<locale>", mirroring the prefix stripLocalePrefix removes.
func withLocalePrefix(bare, locale string) string {
	if locale == "en" {
		return bare
	}
	return "/" + locale + bare
}

// resolveMeta looks up a page-specific "<keyPrefix>_meta_<field>" key, falling
// back to the generic "meta_<field>" key when keyPrefix is empty or the
// specific key isn't present. Callers build keyPrefix for the page kind they
// know about (e.g. "services_<slug>" with hyphens underscored, or the literal
// "privacy"); resolveMeta itself stays agnostic to page kind.
func resolveMeta(translations core.Translations, keyPrefix, field string) (string, bool) {
	if keyPrefix != "" {
		key := keyPrefix + "_meta_" + field
		if value, ok := translations[key].(string); ok {
			return value, true
		}
	}
	value, ok := translations["meta_"+field].(string)
	return value, ok
}

// resolveMetaKeyPrefix builds the specific-key prefix resolveMeta uses for a
// request path (locale prefix included): "services_<slug>" or
// "case_studies_<slug>" for a detail page, the literal "case_studies" for the
// /case-studies index, the literal "privacy" for /privacy, or "" for any other
// page (falls through to the generic meta_<field> keys).
func resolveMetaKeyPrefix(path string) string {
	bare := stripLocalePrefix(path)
	if keyPrefix, isDetail := resolveDetailKeyPrefix(bare); isDetail {
		return keyPrefix
	}
	switch bare {
	case "/case-studies":
		return "case_studies"
	case "/privacy":
		return "privacy"
	}
	return ""
}

// isUnknownDetailPath reports whether path is under a detail section
// (/services/, /case-studies/) but names no id the locale has meta for. The
// locale's per-page meta keys are the source of truth for which ids exist.
func isUnknownDetailPath(path string, translations core.Translations) bool {
	keyPrefix, isDetail := resolveDetailKeyPrefix(stripLocalePrefix(path))
	if !isDetail {
		return false
	}
	if keyPrefix == "" {
		return true
	}
	title, _ := translations[keyPrefix+"_meta_title"].(string)
	return title == ""
}

// siteName is the brand name used in the JSON-LD and social preview tags.
const siteName = "Oni Web Officer"

// siteKnowsAbout is the static, non-translation-driven list of technologies
// advertised in the site-wide Organization JSON-LD schema.
var siteKnowsAbout = []string{
	"Golang",
	"Python",
	"Kafka",
	"Docker",
	"Kubernetes",
	"AWS",
	"Domain-Driven Design",
	"Event-Driven Architecture",
	"Large Language Models",
	"Retrieval-Augmented Generation",
	"AI Agents",
	"Secure SDLC",
	"Threat Modeling",
	"Software Bill of Materials",
}

type organizationSchema struct {
	Context    string   `json:"@context"`
	Type       string   `json:"@type"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	KnowsAbout []string `json:"knowsAbout"`
}

// ogImagePath is the 1200x630 social preview image, served from the
// frontend's dist/ (oniwebsite/public/og-image.png).
const ogImagePath = "/og-image.png"

// ogLocales maps each site locale to its Open Graph locale code.
var ogLocales = map[string]string{"en": "en_US", "pt": "pt_BR", "sv": "sv_SE"}

// socialPreview holds the per-page values LinkedIn, Facebook and X read from
// the raw HTML (they don't run the SPA's JavaScript).
type socialPreview struct {
	title       string
	description string
	url         string
	imageURL    string
	locale      string
}

// socialPreviewTags renders the Open Graph and Twitter card meta tags for p,
// with every value HTML-escaped. Empty title or description tags are omitted.
func socialPreviewTags(p socialPreview) string {
	var b strings.Builder
	property := func(name, content string) {
		if content != "" {
			fmt.Fprintf(&b, "<meta property=\"%s\" content=\"%s\">", name, html.EscapeString(content))
		}
	}
	name := func(name, content string) {
		if content != "" {
			fmt.Fprintf(&b, "<meta name=\"%s\" content=\"%s\">", name, html.EscapeString(content))
		}
	}

	property("og:type", "website")
	property("og:site_name", siteName)
	property("og:title", p.title)
	property("og:description", p.description)
	property("og:url", p.url)
	property("og:image", p.imageURL)
	property("og:image:width", "1200")
	property("og:image:height", "630")
	property("og:image:alt", siteName)
	if ogLocale, ok := ogLocales[p.locale]; ok {
		property("og:locale", ogLocale)
		for _, locale := range siteLocales {
			if locale != p.locale {
				property("og:locale:alternate", ogLocales[locale])
			}
		}
	}
	name("twitter:card", "summary_large_image")
	name("twitter:title", p.title)
	name("twitter:description", p.description)
	name("twitter:image", p.imageURL)
	name("twitter:image:alt", siteName)
	return b.String()
}

func (h *HTMLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lang, ok := r.Context().Value(middleware.CtxLanguageKey).(string)
	if !ok {
		lang = "en"
	}

	// Load Translations
	translations, err := h.Translator.GetTranslations(lang)
	if err != nil {
		// Fallback to English if failing (or handle error logic)
		slog.ErrorContext(r.Context(), "translation load failed, falling back to english", observability.TraceIDAttr(r.Context()), slog.String("lang", lang), slog.Any("error", err))
		observability.CaptureException(r.Context(), err)
		translations, _ = h.Translator.GetTranslations("en")
	}

	jsonBytes, err := json.Marshal(translations)
	if err != nil {
		slog.ErrorContext(r.Context(), "translation marshal failed", observability.TraceIDAttr(r.Context()), slog.String("lang", lang), slog.Any("error", err))
		observability.CaptureException(r.Context(), err)
	}
	jsonString := string(jsonBytes) // The raw JSON to inject

	// Content to Inject
	// We need to parse the HTML and insert:
	// 1. <script id="initial-state">window.__INITIAL_STATE__ = ...</script>
	// 2. <html lang="...">
	// 3. SEO Title/Meta (from translations)

	var htmlContent []byte

	if h.IsDev {
		// Proxy Request to Vite to get the index.html
		// We can't just http.Redirect; we need to fetch the content server-side and then modify it.
		// Or we can use a reverse proxy.
		// But for the HTML file specifically, we want to fetch it, modify it, and return it.
		// For assets (JS/CSS), we will let a separate handler proxy them. This handler is ONLY for the HTML page.

		resp, err := http.Get(h.DevTarget + r.URL.Path) // e.g. http://localhost:5173/ or http://localhost:5173/pt/
		// Note: Vite SPA usually serves index.html for unknown paths.
		// So asking for /pt/ might 404 in Vite unless configured, OR return index.html if using history fallback.
		// Let's assume hitting root "/" of vite returns the template.
		if err != nil || resp.StatusCode != 200 {
			// Try root
			resp, err = http.Get(h.DevTarget + "/")
		}

		if err != nil {
			http.Error(w, "Failed to connect to Vite Dev Server", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		htmlContent = buf.Bytes()

	} else {
		// Production: Read from dist/index.html
		var err error
		htmlContent, err = os.ReadFile(h.DistDir + "/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
	}

	// Replacement Logic (Simple string replacement for now, robust parsing later if needed)
	htmlStr := string(htmlContent)

	// 1. Inject Lang
	htmlStr = strings.Replace(htmlStr, "<html lang=\"en\">", fmt.Sprintf("<html lang=\"%s\">", lang), 1)
	htmlStr = strings.Replace(htmlStr, "<html>", fmt.Sprintf("<html lang=\"%s\">", lang), 1) // Fallback

	// 2. Inject Data
	metaKeyPrefix := resolveMetaKeyPrefix(r.URL.Path)
	injection := fmt.Sprintf("<script>window.__INITIAL_STATE__ = %s;</script>", jsonString)

	// 2a. Meta description
	if metaDescription, ok := resolveMeta(translations, metaKeyPrefix, "description"); ok {
		injection += fmt.Sprintf("<meta name=\"description\" content=\"%s\">", html.EscapeString(metaDescription))
	}

	// 2b. Hreflang alternate links
	scheme := resolveScheme(r)
	bare := stripLocalePrefix(r.URL.Path)
	if bare == "" {
		bare = "/"
	}
	enHref := fmt.Sprintf("%s://%s%s", scheme, r.Host, withLocalePrefix(bare, "en"))
	for _, locale := range siteLocales {
		href := fmt.Sprintf("%s://%s%s", scheme, r.Host, withLocalePrefix(bare, locale))
		injection += fmt.Sprintf("<link rel=\"alternate\" hreflang=\"%s\" href=\"%s\">", locale, href)
	}
	injection += fmt.Sprintf("<link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\">", enHref)

	// 2c. Self-referencing canonical link
	canonicalURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)
	injection += fmt.Sprintf("<link rel=\"canonical\" href=\"%s\">", canonicalURL)

	// 2c'. Social preview (Open Graph + Twitter) tags, reusing the page meta
	metaTitle, _ := resolveMeta(translations, metaKeyPrefix, "title")
	metaDescription, _ := resolveMeta(translations, metaKeyPrefix, "description")
	injection += socialPreviewTags(socialPreview{
		title:       metaTitle,
		description: metaDescription,
		url:         canonicalURL,
		imageURL:    fmt.Sprintf("%s://%s%s", scheme, r.Host, ogImagePath),
		locale:      lang,
	})

	// 2d. Organization JSON-LD
	schema := organizationSchema{
		Context:    "https://schema.org",
		Type:       "Organization",
		Name:       siteName,
		URL:        fmt.Sprintf("%s://%s/", scheme, r.Host),
		KnowsAbout: siteKnowsAbout,
	}
	if schemaBytes, err := json.Marshal(schema); err != nil {
		slog.ErrorContext(r.Context(), "organization schema marshal failed", observability.TraceIDAttr(r.Context()), slog.Any("error", err))
		observability.CaptureException(r.Context(), err)
	} else {
		injection += fmt.Sprintf("<script type=\"application/ld+json\">%s</script>", string(schemaBytes))
	}

	// Inject before </head>
	if strings.Contains(htmlStr, "</head>") {
		htmlStr = strings.Replace(htmlStr, "</head>", injection+"</head>", 1)
	} else {
		htmlStr = htmlStr + injection // Worst case append
	}

	// 3. Inject Title/Meta
	if metaTitle, ok := resolveMeta(translations, metaKeyPrefix, "title"); ok {
		newTitleTag := fmt.Sprintf("<title>%s</title>", html.EscapeString(metaTitle))
		if strings.Contains(htmlStr, "<title>") && strings.Contains(htmlStr, "</title>") {
			start := strings.Index(htmlStr, "<title>")
			end := strings.Index(htmlStr, "</title>") + 8
			htmlStr = htmlStr[:start] + newTitleTag + htmlStr[end:]
		}
	}

	w.Header().Set("Content-Type", "text/html")
	if isUnknownDetailPath(r.URL.Path, translations) {
		// Still serve the SPA so the client can redirect, but tell crawlers
		// the page doesn't exist.
		w.WriteHeader(http.StatusNotFound)
	}
	w.Write([]byte(htmlStr))
}

// DevProxyHandler proxies everything else (assets) to Vite
func DevProxyHandler(target string) http.Handler {
	u, _ := url.Parse(target)
	return httputil.NewSingleHostReverseProxy(u)
}
