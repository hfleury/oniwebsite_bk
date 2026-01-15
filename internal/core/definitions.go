package core

// Translations represents the key-value map for a specific language.
type Translations map[string]interface{}

// TranslationService defines the contract for loading and retrieving translations.
type TranslationService interface {
	// LoadTranslations loads all translation files from the given source.
	LoadTranslations() error
	// GetTranslations returns the translations for a specific language.
	// Returns nil if the language is not found.
	GetTranslations(lang string) (Translations, error)
}
