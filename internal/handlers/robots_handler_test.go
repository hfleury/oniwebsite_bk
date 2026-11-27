package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRobotsHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	RobotsHandler{}.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	want := "User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: https://example.com/sitemap.xml\n"
	if got := w.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
