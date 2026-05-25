package domain

import "strings"

// ResolveAssortmentLanguage converts a BCP-47 locale (e.g. "en-FI") into a
// bare language code ("en") suitable for Wolt's assortment language query
// parameter. Falls back to "en" when the input is empty.
func ResolveAssortmentLanguage(locale string) string {
	language := strings.TrimSpace(locale)
	if idx := strings.Index(language, "-"); idx >= 0 {
		language = strings.TrimSpace(language[:idx])
	}
	if language == "" {
		return "en"
	}
	return language
}
