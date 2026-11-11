package handlers

import (
	"fmt"
	"net/http"
)

// RobotsHandler serves /robots.txt: every crawler, AI crawlers included, may
// index everything except the translation API. Cloudflare prepends its managed
// content-signals block to this file in production.
type RobotsHandler struct{}

func (RobotsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: %s://%s/sitemap.xml\n", resolveScheme(r), r.Host)
}
