package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"oniwebsite_bk/internal/core"
	"oniwebsite_bk/internal/observability"
)

type TranslationHandler struct {
	Translator core.TranslationService
}

func NewTranslationHandler(t core.TranslationService) *TranslationHandler {
	return &TranslationHandler{
		Translator: t,
	}
}

func (h *TranslationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Simple query param ?lang=pt
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	translations, err := h.Translator.GetTranslations(lang)
	if err != nil {
		// Fallback to en
		var fallbackErr error
		translations, fallbackErr = h.Translator.GetTranslations("en")
		if fallbackErr != nil {
			slog.ErrorContext(r.Context(), "translation fallback to english failed", observability.TraceIDAttr(r.Context()), slog.String("lang", lang), slog.Any("error", fallbackErr))
			observability.CaptureException(r.Context(), fallbackErr)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	// If needed later, we can add CORS here explicitly,
	// but purely relying on Vite proxy is cleaner for dev.
	json.NewEncoder(w).Encode(translations)
}
