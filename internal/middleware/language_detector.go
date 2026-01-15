package middleware

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/text/language"
)

type LanguageKey string

const CtxLanguageKey LanguageKey = "language"

// LanguageDetectorMiddleware checks the URL path and Accept-Language header.
// It sets the determined language in the context.
// Supported languages: "en", "pt", "sv".
func LanguageDetectorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 1. Check URL Prefix
		if strings.HasPrefix(path, "/pt") {
			ctx := context.WithValue(r.Context(), CtxLanguageKey, "pt")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if strings.HasPrefix(path, "/sv") {
			ctx := context.WithValue(r.Context(), CtxLanguageKey, "sv")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 2. Check Root Path for Redirection (only on exact root or /index.html)
		// If we are at root "/", we check browser string.
		// If browser prefers PT or SV, we redirect.
		// Otherwise we fall through to English (default).
		if path == "/" || path == "/index.html" {
			accept := r.Header.Get("Accept-Language")
			matcher := language.NewMatcher([]language.Tag{
				language.English, // The first language is the fallback
				language.Portuguese,
				language.Swedish,
			})
			tag, _, _ := matcher.Match(language.Make(accept))
			base, _ := tag.Base()

			langCode := base.String()

			// If detected is strictly PT or SV, redirect.
			if langCode == "pt" {
				http.Redirect(w, r, "/pt/", http.StatusFound)
				return
			}
			if langCode == "sv" {
				http.Redirect(w, r, "/sv/", http.StatusFound)
				return
			}
		}

		// Default to English
		ctx := context.WithValue(r.Context(), CtxLanguageKey, "en")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
