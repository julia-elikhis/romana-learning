package materials

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Deterministic source recall, not a language model or a grammar-correctness judge.
const GeneratorVersion = "source-recall-v1"

type Draft struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Answers     []string `json:"answers"`
	Explanation string   `json:"explanation"`
	SourceQuote string   `json:"sourceQuote"`
	SourceLine  int      `json:"sourceLine"`
	Status      string   `json:"status"`
}

var words = regexp.MustCompile(`[\p{L}]+(?:[-’'][\p{L}]+)*`)
var sentenceSplit = regexp.MustCompile(`[.!?;]+\s+`)
var verbGroups = [][]string{
	{"sunt", "ești", "este", "suntem", "sunteți"},
	{"am", "ai", "are", "avem", "aveți", "au"},
	{"stau", "stai", "stă", "stăm", "stați"},
	{"merg", "mergi", "merge", "mergem", "mergeți"},
	{"fac", "faci", "face", "facem", "faceți"},
	{"lucrez", "lucrezi", "lucrează", "lucrăm", "lucrați"},
}
var functionWords = map[string]bool{"pentru": true, "care": true, "este": true, "sunt": true, "când": true, "unde": true, "fiecare": true, "foarte": true, "niște": true, "acest": true, "această": true, "aceste": true, "despre": true, "după": true, "exemplu": true, "exemple": true, "completați": true, "exercițiu": true, "scrieți": true, "răspundeți": true}

func Normalize(s string) string {
	s = norm.NFC.String(s)
	s = strings.NewReplacer("ş", "ș", "ţ", "ț", "Ş", "Ș", "Ţ", "Ț").Replace(s)
	return strings.ToLower(strings.Trim(strings.Join(strings.Fields(s), " "), " .!?\"„”"))
}
func Generate(text, materialID string, limit int) ([]Draft, error) {
	if limit < 1 || limit > 20 {
		return nil, errors.New("Choose between 1 and 20 exercises")
	}
	drafts := []Draft{}
	seen := map[string]bool{}
	for n, line := range strings.Split(text, "\n") {
		for _, part := range sentenceSplit.Split(line, -1) {
			quote := strings.TrimSpace(part)
			if len(quote) < 15 || len(quote) > 350 || strings.ContainsAny(quote, "@<>_=|/#0123456789") {
				continue
			}
			// Skip bilingual/markup rows and imperative instructions: these are not clean examples.
			invalid := false
			for _, r := range quote {
				if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
					invalid = true
				}
			}
			if invalid {
				continue
			}
			spans := words.FindAllStringIndex(quote, -1)
			if len(spans) < 4 || len(spans) > 30 {
				continue
			}
			first := Normalize(quote[spans[0][0]:spans[0][1]])
			if functionWords[first] && (first == "completați" || first == "scrieți" || first == "răspundeți" || strings.HasPrefix(first, "exempl")) {
				continue
			}
			selected := -1
			var group []string
			for i, span := range spans {
				token := Normalize(quote[span[0]:span[1]])
				for _, g := range verbGroups {
					for _, v := range g {
						if v == token {
							selected = i
							group = g
							break
						}
					}
					if selected >= 0 {
						break
					}
				}
				if selected >= 0 {
					break
				}
			}
			// For other sentences, select a substantive lower-case Romanian word.
			if selected < 0 && strings.ContainsAny(quote, "ăâîșțĂÂÎȘȚ") {
				for i := len(spans) - 1; i > 0; i-- {
					token := quote[spans[i][0]:spans[i][1]]
					r := []rune(token)
					if len(r) >= 4 && unicode.IsLower(r[0]) && !functionWords[Normalize(token)] {
						selected = i
						break
					}
				}
			}
			if selected < 0 {
				continue
			}
			span := spans[selected]
			answer := quote[span[0]:span[1]]
			prompt := "Complete the sentence as it appears in your notes:\n" + quote[:span[0]] + "____" + quote[span[1]:]
			if seen[Normalize(prompt)] {
				continue
			}
			seen[Normalize(prompt)] = true
			kind := "cloze"
			options := []string{}
			if len(group) > 0 && len(drafts)%2 == 0 {
				kind = "multiple_choice"
				options = append(options, answer)
				for _, v := range group {
					if Normalize(v) != Normalize(answer) {
						options = append(options, v)
					}
					if len(options) == 3 {
						break
					}
				}
				// Stable rotation avoids placing every correct answer first.
				shift := n % len(options)
				options = append(options[shift:], options[:shift]...)
			}
			hash := sha256.Sum256([]byte(materialID + "\x00" + GeneratorVersion + "\x00" + prompt))
			drafts = append(drafts, Draft{ID: "generated-" + hex.EncodeToString(hash[:16]), Kind: kind, Prompt: prompt, Options: options, Answers: []string{answer}, Explanation: fmt.Sprintf("Your notes use “%s” here. Read the full sentence, then recall it again.", answer), SourceQuote: quote, SourceLine: n + 1, Status: "draft"})
			if len(drafts) == limit {
				return drafts, nil
			}
		}
	}
	if len(drafts) == 0 {
		return nil, errors.New("No clean Romanian examples found. Edit the extracted text into short Romanian sentences, one per line, then review it again")
	}
	return drafts, nil
}
func Validate(d Draft) error {
	for _, value := range append(append([]string{d.Prompt, d.Explanation, d.SourceQuote}, d.Answers...), d.Options...) {
		if strings.ContainsRune(value, 0) {
			return errors.New("Exercise text contains an invalid character")
		}
	}

	if d.Kind != "cloze" && d.Kind != "multiple_choice" {
		return errors.New("Choose cloze or multiple_choice")
	}
	if len(strings.TrimSpace(d.Prompt)) < 5 || len(d.Prompt) > 1000 || strings.Count(d.Prompt, "____") != 1 {
		return errors.New("The prompt needs a ____ gap and must be under 1,000 characters")
	}
	if len(d.Answers) < 1 || len(d.Answers) > 5 {
		return errors.New("Provide 1–5 accepted answers")
	}
	for _, a := range d.Answers {
		if Normalize(a) == "" || len(a) > 200 {
			return errors.New("Invalid accepted answer")
		}
	}
	if !strings.Contains(Normalize(d.SourceQuote), Normalize(d.Answers[0])) {
		return errors.New("The primary answer must appear in the cited source")
	}
	if len(d.Explanation) > 1500 || strings.TrimSpace(d.Explanation) == "" {
		return errors.New("Provide a short explanation")
	}
	if d.Kind == "cloze" && len(d.Options) != 0 {
		return errors.New("Cloze exercises do not have answer options")
	}
	if d.Kind == "multiple_choice" {
		if len(d.Options) < 2 || len(d.Options) > 5 || len(d.Answers) != 1 {
			return errors.New("Multiple choice needs 2–5 options and one answer")
		}
		seen := map[string]bool{}
		match := false
		for _, o := range d.Options {
			k := Normalize(o)
			if k == "" || len(o) > 200 || seen[k] {
				return errors.New("Options must be nonempty and distinct")
			}
			seen[k] = true
			if k == Normalize(d.Answers[0]) {
				match = true
			}
		}
		if !match {
			return errors.New("The correct answer must be an option")
		}
	}
	return nil
}
