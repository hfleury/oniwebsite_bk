package handlers

import (
	"encoding/json"
	"net/http"
	"oniwebsite_bk/internal/core"
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
		translations, _ = h.Translator.GetTranslations("en")
	}

	w.Header().Set("Content-Type", "application/json")
	// If needed later, we can add CORS here explicitly,
	// but purely relying on Vite proxy is cleaner for dev.
	json.NewEncoder(w).Encode(translations)
}
