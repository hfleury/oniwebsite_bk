package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/middleware"
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

func (h *HTMLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lang, ok := r.Context().Value(middleware.CtxLanguageKey).(string)
	if !ok {
		lang = "en"
	}

	// Load Translations
	translations, err := h.Translator.GetTranslations(lang)
	if err != nil {
		// Fallback to English if failing (or handle error logic)
		fmt.Printf("Error loading translations for %s: %v\n", lang, err)
		translations, _ = h.Translator.GetTranslations("en")
	}

	jsonBytes, _ := json.Marshal(translations)
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
	injection := fmt.Sprintf("<script>window.__INITIAL_STATE__ = %s;</script>", jsonString)
	// Inject before </head>
	if strings.Contains(htmlStr, "</head>") {
		htmlStr = strings.Replace(htmlStr, "</head>", injection+"</head>", 1)
	} else {
		htmlStr = htmlStr + injection // Worst case append
	}

	// 3. Inject Title/Meta
	if heroTitle, ok := translations["hero_title"].(string); ok {
		// Replace standard title if present
		// Assuming index.html has <title>Vite + React + TS</title> or similar
		// We will matching <title>...</title> loosely
		// Simple approach: Replace <title>.*</title> with <title>New Title</title>
		// Note: Regex would be better but expensive-ish.
		// Let's just do a specific replacement of the default title if we know it.
		// OR just inject it if we can.

		// For now, to satisfy the compiler and do something useful:
		newTitleTag := fmt.Sprintf("<title>%s</title>", heroTitle)
		if strings.Contains(htmlStr, "<title>") && strings.Contains(htmlStr, "</title>") {
			// Find start and end generic
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
