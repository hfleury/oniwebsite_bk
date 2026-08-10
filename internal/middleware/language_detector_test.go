package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type nextCall struct {
	called bool
	lang   any
}

func newRecordingNext(rec *nextCall) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.called = true
		rec.lang = r.Context().Value(CtxLanguageKey)
		w.WriteHeader(http.StatusOK)
	})
}

func TestLanguageDetectorMiddleware_PathPrefixPT(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/pt/about", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "pt" {
		t.Errorf("context language = %v, want %q", rec.lang, "pt")
	}
}

func TestLanguageDetectorMiddleware_PathPrefixSV(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/sv/about", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "sv" {
		t.Errorf("context language = %v, want %q", rec.lang, "sv")
	}
}

func TestLanguageDetectorMiddleware_BarePTPrefix(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/pt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "pt" {
		t.Errorf("context language = %v, want %q", rec.lang, "pt")
	}
}

func TestLanguageDetectorMiddleware_RootRedirectsToPT(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "pt")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if rec.called {
		t.Fatal("expected next handler NOT to be called")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got, want := w.Header().Get("Location"), "/pt/"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestLanguageDetectorMiddleware_RootRedirectsToSV(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "sv")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if rec.called {
		t.Fatal("expected next handler NOT to be called")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got, want := w.Header().Get("Location"), "/sv/"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestLanguageDetectorMiddleware_RootFallsThroughToEnglish(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "en" {
		t.Errorf("context language = %v, want %q", rec.lang, "en")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLanguageDetectorMiddleware_RootNoAcceptLanguageDefaultsEnglish(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "en" {
		t.Errorf("context language = %v, want %q", rec.lang, "en")
	}
}

func TestLanguageDetectorMiddleware_IndexHTMLBehavesLikeRoot(t *testing.T) {
	t.Run("redirects to pt", func(t *testing.T) {
		rec := &nextCall{}
		handler := LanguageDetectorMiddleware(newRecordingNext(rec))

		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		req.Header.Set("Accept-Language", "pt")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if rec.called {
			t.Fatal("expected next handler NOT to be called")
		}
		if w.Code != http.StatusFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
		}
		if got, want := w.Header().Get("Location"), "/pt/"; got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}
	})

	t.Run("falls through to english", func(t *testing.T) {
		rec := &nextCall{}
		handler := LanguageDetectorMiddleware(newRecordingNext(rec))

		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		req.Header.Set("Accept-Language", "en")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if !rec.called {
			t.Fatal("expected next handler to be called")
		}
		if rec.lang != "en" {
			t.Errorf("context language = %v, want %q", rec.lang, "en")
		}
	})
}

func TestLanguageDetectorMiddleware_MultiTagAcceptLanguageUsesFirstTag(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,pt;q=0.8")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called (no redirect)")
	}
	if rec.lang != "en" {
		t.Errorf("context language = %v, want %q", rec.lang, "en")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLanguageDetectorMiddleware_UnrecognizedPathDefaultsEnglish(t *testing.T) {
	rec := &nextCall{}
	handler := LanguageDetectorMiddleware(newRecordingNext(rec))

	req := httptest.NewRequest(http.MethodGet, "/about", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !rec.called {
		t.Fatal("expected next handler to be called")
	}
	if rec.lang != "en" {
		t.Errorf("context language = %v, want %q", rec.lang, "en")
	}
}
