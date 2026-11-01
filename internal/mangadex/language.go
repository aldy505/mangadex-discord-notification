package mangadex

import (
	"fmt"
	"strings"

	"github.com/emvi/iso-639-1"
)

// LanguageKey is an ISO 639-1 language code as used by the MangaDex API.
type LanguageKey string

func (l LanguageKey) IsValid() bool {
	return iso6391.ValidCode(string(l))
}

// NormalizeLanguages lowercases, trims, de-duplicates and validates a list of
// language codes. An empty input yields English.
func NormalizeLanguages(raw []string) ([]LanguageKey, error) {
	if len(raw) == 0 {
		return []LanguageKey{"en"}, nil
	}

	seen := make(map[LanguageKey]struct{}, len(raw))
	languages := make([]LanguageKey, 0, len(raw))
	for _, entry := range raw {
		code := LanguageKey(strings.ToLower(strings.TrimSpace(entry)))
		if code == "" {
			continue
		}
		if !code.IsValid() {
			return nil, fmt.Errorf("invalid language code %q", entry)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		languages = append(languages, code)
	}

	if len(languages) == 0 {
		return []LanguageKey{"en"}, nil
	}

	return languages, nil
}
