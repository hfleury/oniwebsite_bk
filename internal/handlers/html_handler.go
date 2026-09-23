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

// extractServiceSlug returns the first path segment after "/services/" in
// path (locale prefix stripped first), or "" if path isn't a service page.
func extractServiceSlug(path string) string {
	path = stripLocalePrefix(path)
	const prefix = "/services/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
		rest = rest[:slashIdx]
	}
	return rest
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
// request path (locale prefix included): "services_<slug>" for a service
// page, the literal "privacy" for /privacy, or "" for any other page (falls
// through to the generic meta_<field> keys).
func resolveMetaKeyPrefix(path string) string {
	if slug := extractServiceSlug(path); slug != "" {
		return "services_" + strings.ReplaceAll(slug, "-", "_")
	}
	if stripLocalePrefix(path) == "/privacy" {
		return "privacy"
	}
	return ""
}

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
}

type organizationSchema struct {
	Context    string   `json:"@context"`
	Type       string   `json:"@type"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	KnowsAbout []string `json:"knowsAbout"`
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
	for _, locale := range []string{"en", "pt", "sv"} {
		href := fmt.Sprintf("%s://%s%s", scheme, r.Host, withLocalePrefix(bare, locale))
		injection += fmt.Sprintf("<link rel=\"alternate\" hreflang=\"%s\" href=\"%s\">", locale, href)
	}
	injection += fmt.Sprintf("<link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\">", enHref)

	// 2c. Organization JSON-LD
	schema := organizationSchema{
		Context:    "https://schema.org",
		Type:       "Organization",
		Name:       "Oni Web Officer",
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
		newTitleTag := fmt.Sprintf("<title>%s</title>", metaTitle)
		if strings.Contains(htmlStr, "<title>") && strings.Contains(htmlStr, "</title>") {
			start := strings.Index(htmlStr, "<title>")
			end := strings.Index(htmlStr, "</title>") + 8
			htmlStr = htmlStr[:start] + newTitleTag + htmlStr[end:]
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlStr))
}

// DevProxyHandler proxies everything else (assets) to Vite
func DevProxyHandler(target string) http.Handler {
	u, _ := url.Parse(target)
	return httputil.NewSingleHostReverseProxy(u)
}
