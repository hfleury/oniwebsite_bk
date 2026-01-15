package services

import (
	"encoding/json"
	"fmt"
	"oniwebsite_bk/internal/core"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FileTranslationService struct {
	localesDir string
	cache      map[string]core.Translations
	mu         sync.RWMutex
}

func NewFileTranslationService(localesDir string) *FileTranslationService {
	return &FileTranslationService{
		localesDir: localesDir,
		cache:      make(map[string]core.Translations),
	}
}

func (s *FileTranslationService) LoadTranslations() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := os.ReadDir(s.localesDir)
	if err != nil {
		return fmt.Errorf("failed to read locales directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		lang := strings.TrimSuffix(file.Name(), ".json")
		filePath := filepath.Join(s.localesDir, file.Name())

		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file.Name(), err)
		}

		var translations core.Translations
		if err := json.Unmarshal(content, &translations); err != nil {
			return fmt.Errorf("failed to parse json %s: %w", file.Name(), err)
		}

		s.cache[lang] = translations
	}

	return nil
}

func (s *FileTranslationService) GetTranslations(lang string) (core.Translations, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.cache[lang]
	if !ok {
		// Fallback to "en" if not found? Or return error.
		// For now, let's return error so caller can decide fallback (e.g. English).
		// But in a practical i18n system, we might want a chain.
		// Let's stick to strict retrieval for this method.
		return nil, fmt.Errorf("translations not found for language: %s", lang)
	}
	return data, nil
}
