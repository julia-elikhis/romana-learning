package materials

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
)

type GenerationSummary struct {
	Requested       map[string]int `json:"requested"`
	Created         map[string]int `json:"created"`
	Shortfall       map[string]int `json:"shortfall"`
	RefillAttempted bool           `json:"refillAttempted"`
	Note            string         `json:"note,omitempty"`
}

// Small batches still include both recognition and recall; batches of three or
// more also include multi-select. For 20 questions the target is 10 / 8 / 2.
func PracticeMix(count int) map[string]int {
	mix := map[string]int{"multiple_choice": 0, "cloze": 0, "multi_select": 0}
	if count < 1 {
		return mix
	}
	if count == 1 {
		mix["cloze"] = 1
		return mix
	}
	mix["cloze"] = (count*4 + 5) / 10
	if count >= 3 {
		mix["multi_select"] = max(1, count/10)
	}
	mix["multiple_choice"] = count - mix["cloze"] - mix["multi_select"]
	return mix
}

type generationSlot struct {
	Kind           string `json:"kind"`
	PreferredSkill string `json:"preferredSkill"`
	Difficulty     string `json:"difficulty"`
}

func generationSlots(mix map[string]int) []generationSlot {
	slots := []generationSlot{}
	for _, kind := range []string{"multiple_choice", "cloze", "multi_select"} {
		skills := []string{"grammar", "vocabulary", "communication", "reading"}
		if kind == "cloze" {
			skills = []string{"grammar", "vocabulary"}
		}
		if kind == "multi_select" {
			skills = []string{"grammar", "vocabulary", "reading"}
		}
		for i := 0; i < mix[kind]; i++ {
			slots = append(slots, generationSlot{kind, skills[i%len(skills)], []string{"easy", "medium", "medium", "hard", "medium"}[i%5]})
		}
	}
	return slots
}

func (a *API) generateMixed(parent context.Context, source, original, materialID string, count int) (GenerationResult, error) {
	// Keep the existing overall request deadline. Reserve time for saving already
	// reviewed drafts even if an optional replacement request uses its time budget.
	budget := time.Now().Add(GenerationTimeout - 10*time.Second)
	if deadline, ok := parent.Deadline(); ok && deadline.Add(-10*time.Second).Before(budget) {
		budget = deadline.Add(-10 * time.Second)
	}
	ctx, cancel := context.WithDeadline(parent, budget)
	defer cancel()
	result := GenerationResult{Exercises: []Draft{}, Skipped: []GenerationIssue{}, positions: map[string]int{}, Summary: GenerationSummary{Requested: PracticeMix(count), Created: PracticeMix(0), Shortfall: PracticeMix(0)}}
	remaining := PracticeMix(count)
	seen := map[string]bool{}
	targets := map[string]int{}
	offset := 0
	var lastError error
	for round := 0; round < 2; round++ {
		total := 0
		for _, n := range remaining {
			total += n
		}
		if total == 0 {
			break
		}
		if round == 1 {
			if time.Until(budget) < 15*time.Second {
				result.Summary.Note = "The replacement time budget was reached; only reviewed questions were saved."
				break
			}
			result.Summary.RefillAttempted = true
		}
		avoid := []string{}
		for _, d := range result.Exercises {
			avoid = append(avoid, d.Prompt)
		}
		input, _ := json.Marshal(map[string]any{"teachingText": source, "requestedCounts": remaining, "slots": generationSlots(remaining), "alreadyAcceptedPrompts": avoid})
		content, err := a.chat(ctx, exerciseInstruction, string(input))
		if err != nil {
			lastError = err
			break
		}
		var output struct {
			Exercises []Draft `json:"exercises"`
		}
		if json.Unmarshal([]byte(content), &output) != nil || len(output.Exercises) > total {
			lastError = errors.New("Generation API returned invalid exercise JSON")
			break
		}
		if len(output.Exercises) == 0 {
			lastError = errors.New("The lesson did not support additional questions")
			break
		}
		validated, validationErr := validateGenerated(output.Exercises, original, materialID)
		for _, issue := range validated.Skipped {
			issue.Question += offset
			result.Skipped = append(result.Skipped, issue)
		}
		if validationErr != nil && len(validated.Skipped) == 0 {
			lastError = validationErr
			break
		}
		candidates := GenerationResult{Exercises: []Draft{}, Skipped: []GenerationIssue{}, positions: map[string]int{}}
		taken := PracticeMix(0)
		roundTargets := map[string]int{}
		for _, d := range validated.Exercises {
			number := validated.positions[d.ID] + offset
			reason := ""
			target := d.Skill + ":" + Normalize(d.Target)
			switch {
			case d.Skill == "" || strings.TrimSpace(d.Target) == "" || d.Difficulty == "":
				reason = "A generated question needs a skill, specific learning target, and difficulty"
			case seen[Normalize(d.Prompt)]:
				reason = "This question duplicates an already accepted question"
			case taken[d.Kind] >= remaining[d.Kind]:
				reason = "This question exceeds the requested count for its format"
			case targets[target]+roundTargets[target] >= 2:
				reason = "This specific learning target already has two questions"
			}
			if reason != "" {
				result.Skipped = append(result.Skipped, GenerationIssue{number, reason})
				continue
			}
			taken[d.Kind]++
			roundTargets[target]++
			candidates.Exercises = append(candidates.Exercises, d)
			candidates.positions[d.ID] = number
		}
		offset += len(output.Exercises)
		if len(candidates.Exercises) == 0 {
			lastError = validationErr
			continue
		}
		reviewed, err := a.reviewRomanian(ctx, source, candidates)
		// All rejected is a valid review outcome and can be refilled. Invalid or
		// incomplete reviewer output never allows the candidate batch to be saved.
		if err != nil && len(reviewed.Skipped) == 0 {
			lastError = err
			break
		}
		result.Skipped = append(result.Skipped, reviewed.Skipped...)
		lastError = err
		for _, d := range reviewed.Exercises {
			// Keys contain full option text, so shuffling after approval changes
			// presentation without changing the reviewed question or its grading.
			rand.Shuffle(len(d.Options), func(i, j int) { d.Options[i], d.Options[j] = d.Options[j], d.Options[i] })
			result.Exercises = append(result.Exercises, d)
			seen[Normalize(d.Prompt)] = true
			targets[d.Skill+":"+Normalize(d.Target)]++
			result.positions[d.ID] = reviewed.positions[d.ID]
			remaining[d.Kind]--
			result.Summary.Created[d.Kind]++
		}
	}
	if len(result.Exercises) == 0 {
		if lastError != nil {
			return GenerationResult{}, lastError
		}
		return GenerationResult{}, errors.New("No questions passed the format and Romanian checks. Nothing was saved; review the lesson and retry")
	}
	result.Summary.Shortfall = remaining
	for _, n := range remaining {
		if n > 0 && result.Summary.Note == "" {
			result.Summary.Note = "The full requested mix could not be completed. Only questions that passed both checks were saved."
		}
	}
	return result, nil
}
