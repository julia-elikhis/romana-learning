package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func prepareAttempt(a *Attempt) bool {
	if a.Selections == nil {
		return materials.Normalize(a.Answer) != "" && len(a.Answer) <= 200 && !strings.ContainsRune(a.Answer, 0)
	}
	if a.Answer != "" || len(a.Selections) == 0 || len(a.Selections) > 5 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range a.Selections {
		if value == "" || len(value) > 200 || strings.ContainsRune(value, 0) || seen[value] {
			return false
		}
		seen[value] = true
	}
	slices.Sort(a.Selections)
	a.Answer = strings.Join(a.Selections, " · ")
	return true
}

// Multi-select is exact-set grading. Selecting every option, missing a correct
// option, or adding an incorrect option never earns correctness or leaderboard credit.
func gradeAttempt(d materials.Draft, a Attempt) (bool, string, []string, error) {
	if d.Kind == "multi_select" {
		if len(a.Selections) == 0 {
			return false, "", nil, errors.New("Select the correct options")
		}
		for _, value := range a.Selections {
			if !slices.Contains(d.Options, value) {
				return false, "", nil, errors.New("Choose available options")
			}
		}
		correctOptions := []string{}
		for _, option := range d.Options {
			for _, answer := range d.Answers {
				if materials.Normalize(option) == materials.Normalize(answer) {
					correctOptions = append(correctOptions, option)
					break
				}
			}
		}
		slices.Sort(correctOptions)
		return slices.Equal(correctOptions, a.Selections), strings.Join(correctOptions, " · "), correctOptions, nil
	}
	if a.Selections != nil {
		return false, "", nil, errors.New("This question accepts one answer")
	}
	if d.Kind == "multiple_choice" && !slices.Contains(d.Options, a.Answer) {
		return false, "", nil, errors.New("Choose an available answer")
	}
	for _, answer := range d.Answers {
		if materials.Normalize(answer) == materials.Normalize(a.Answer) {
			return true, d.Answers[0], nil, nil
		}
	}
	return false, d.Answers[0], nil, nil
}
