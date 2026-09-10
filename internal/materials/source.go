package materials

import (
	"strings"
	"unicode"
)

func latinText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return true
}

// GenerationSource removes non-Latin annotations (including Hebrew) before
// either model request. Keep Latin fragments verbatim and separated, so mixed
// lines retain their Romanian examples without joining across removed text.
// Prompts and the independent reviewer handle other languages in Latin script.
// Evidence locations are always resolved against the untouched original text.
func GenerationSource(source string) string {
	var out strings.Builder
	omitting := false
	for _, r := range source {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			if !omitting {
				out.WriteRune('\n')
			}
			omitting = true
			continue
		}
		if unicode.Is(unicode.Cf, r) || (omitting && unicode.IsMark(r)) {
			continue
		}
		if omitting && r != '\n' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		omitting = false
		out.WriteRune(r)
	}
	return out.String()
}
