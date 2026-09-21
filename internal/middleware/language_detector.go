package middleware

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/text/language"
)

type LanguageKey string

const CtxLanguageKey LanguageKey = "language"

// LanguageCookieName is the cookie the language dropdown sets when a visitor
// explicitly picks a language. It only affects the root path (see below).
const LanguageCookieName = "lang"

// preferredLanguage returns the language to use at the root path: the
// language cookie wins when it holds a supported value, otherwise the
// Accept-Language header decides, defaulting to English.
func preferredLanguage(r *http.Request) string {
	if cookie, err := r.Cookie(LanguageCookieName); err == nil {
		switch cookie.Value {
		case "en", "pt", "sv":
			return cookie.Value
		}
	}

	matcher := language.NewMatcher([]language.Tag{
		language.English, // The first language is the fallback
		language.Portuguese,
		language.Swedish,
	})
	tag, _, _ := matcher.Match(language.Make(r.Header.Get("Accept-Language")))
	base, _ := tag.Base()
	return base.String()
}

// LanguageDetectorMiddleware checks the URL path, the language cookie and the
// Accept-Language header. It sets the determined language in the context.
// The URL always wins; the cookie and header only pick the language at the
// root path. Supported languages: "en", "pt", "sv".
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
		// If we are at root "/", we check the language cookie, then the browser string.
		// If the visitor prefers PT or SV, we redirect.
		// Otherwise we fall through to English (default).
		if path == "/" || path == "/index.html" {
			// Both the redirect and the English page depend on these headers,
			// so any cache in front of the site must key on them.
			w.Header().Set("Vary", "Accept-Language, Cookie")

			langCode := preferredLanguage(r)

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
