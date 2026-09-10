package materials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPracticeMixCounts(t *testing.T) {
	for n := 1; n <= 20; n++ {
		mix := PracticeMix(n)
		sum := 0
		for _, v := range mix {
			sum += v
		}
		if sum != n || mix["cloze"] < 1 || n > 1 && mix["multiple_choice"] < 1 || n > 2 && mix["multi_select"] < 1 {
			t.Fatalf("Invalid mix for %d: %v", n, mix)
		}
	}
	if !reflect.DeepEqual(PracticeMix(20), map[string]int{"multiple_choice": 10, "cloze": 8, "multi_select": 2}) {
		t.Fatal("Unexpected default mix")
	}
}

func mixedFixture(kind string, n int) Draft {
	d := Draft{Kind: kind, Prompt: fmt.Sprintf("Use a fi, example %d: Noi ____ acasă.", n), Answers: []string{"suntem"}, Options: []string{}, Explanation: "Noi takes suntem, meaning we are.", SourceQuote: "Noi suntem acasă.", Skill: "grammar", Target: fmt.Sprintf("test target %d", n), Difficulty: "medium"}
	if kind == "multiple_choice" {
		d.Prompt = fmt.Sprintf("Question %d: Which form of a fi agrees with noi?", n)
		d.Options = []string{"suntem", "sunt", "este"}
	}
	if kind == "multi_select" {
		d.Prompt = fmt.Sprintf("Question %d: Select all correct sentences.", n)
		d.Options = []string{"Noi suntem acasă.", "Eu sunt acasă.", "Tu este acasă."}
		d.Answers = d.Options[:2]
	}
	return d
}

func TestMixedGenerationRefillsOnlyRejectedFormatAndReviewsReplacement(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct{ Messages []struct{ Content string } }
		json.NewDecoder(r.Body).Decode(&request)
		var output any
		if request.Messages[0].Content == exerciseInstruction {
			var input struct {
				RequestedCounts        map[string]int
				AlreadyAcceptedPrompts []string
				Slots                  []generationSlot
			}
			if json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil {
				t.Fatal("Invalid plan")
			}
			if calls == 1 {
				if !reflect.DeepEqual(input.RequestedCounts, PracticeMix(5)) || len(input.Slots) != 5 {
					t.Fatal("No concrete format plan")
				}
				output = map[string]any{"exercises": []Draft{mixedFixture("multiple_choice", 1), mixedFixture("multiple_choice", 2), mixedFixture("cloze", 3), mixedFixture("cloze", 4), mixedFixture("multi_select", 5)}}
			} else {
				if calls != 3 || input.RequestedCounts["multiple_choice"] != 1 || input.RequestedCounts["cloze"] != 0 || input.RequestedCounts["multi_select"] != 0 || len(input.AlreadyAcceptedPrompts) != 4 {
					t.Fatal("Replacement request was not limited to missing format")
				}
				output = map[string]any{"exercises": []Draft{mixedFixture("multiple_choice", 6)}}
			}
		} else {
			if request.Messages[0].Content != romanianReviewInstruction || len(request.Messages) != 2 {
				t.Fatal("Review was not independent")
			}
			var input struct{ Questions []Draft }
			json.Unmarshal([]byte(request.Messages[1].Content), &input)
			reviews := []languageVerdict{}
			for _, q := range input.Questions {
				approved := q.Target != "test target 2"
				issues := []string{}
				if !approved {
					issues = []string{"ambiguity"}
				}
				reviews = append(reviews, languageVerdict{q.ID, &approved, issues})
			}
			output = map[string]any{"reviews": reviews}
		}
		raw, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(raw)}}}})
	}))
	defer server.Close()
	api, _ := NewAPI(server.URL, "", "test-model")
	api.client.Transport = server.Client().Transport
	result, err := api.Generate(context.Background(), "Noi suntem acasă.", "lesson", 5)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || len(result.Exercises) != 5 || !result.Summary.RefillAttempted || !reflect.DeepEqual(result.Summary.Created, PracticeMix(5)) {
		t.Fatal("Mixed generation or refill failed")
	}
	for _, n := range result.Summary.Shortfall {
		if n != 0 {
			t.Fatal("Completed mix reports a shortfall")
		}
	}
	for _, d := range result.Exercises {
		if d.Target == "test target 2" || d.Status != "draft" {
			t.Fatal("Rejected content or publication status survived")
		}
	}
}

func TestMixedGenerationDoesNotReplaceChoiceQuotaWithMoreCloze(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct{ Messages []struct{ Content string } }
		json.NewDecoder(r.Body).Decode(&req)
		var output any
		if req.Messages[0].Content == exerciseInstruction {
			output = map[string]any{"exercises": []Draft{mixedFixture("cloze", calls*10), mixedFixture("cloze", calls*10+1)}}
		} else {
			var input struct{ Questions []Draft }
			json.Unmarshal([]byte(req.Messages[1].Content), &input)
			reviews := []languageVerdict{}
			for _, q := range input.Questions {
				reviews = append(reviews, languageVerdict{q.ID, boolPointer(true), []string{}})
			}
			output = map[string]any{"reviews": reviews}
		}
		raw, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(raw)}}}})
	}))
	defer server.Close()
	api, _ := NewAPI(server.URL, "", "test-model")
	api.client.Transport = server.Client().Transport
	result, err := api.Generate(context.Background(), "Noi suntem acasă.", "lesson", 5)
	if err != nil || calls != 3 || len(result.Exercises) != 2 || result.Summary.Created["cloze"] != 2 || result.Summary.Shortfall["multiple_choice"] != 2 || result.Summary.Shortfall["multi_select"] != 1 || result.Summary.Note == "" {
		t.Fatalf("Incorrect shortfall or retry count: %v", err)
	}
}

func TestMultiSelectValidationAndUnconstrainedChoicePrompts(t *testing.T) {
	source := "Noi suntem acasă."
	for _, kind := range []string{"multiple_choice", "multi_select"} {
		d := mixedFixture(kind, 1)
		if _, err := validateGenerated([]Draft{d}, source, "lesson"); err != nil {
			t.Fatalf("Non-gap %s was rejected: %v", kind, err)
		}
	}
	valid := mixedFixture("multi_select", 1)
	for name, answers := range map[string][]string{"single": valid.Answers[:1], "all": valid.Options, "duplicate": {valid.Answers[0], valid.Answers[0]}, "missing option": {valid.Answers[0], "Not an option"}} {
		t.Run(name, func(t *testing.T) {
			d := valid
			d.Answers = answers
			if Validate(d) == nil {
				t.Fatal("Invalid multi-select key accepted")
			}
		})
	}
	d := valid
	d.Options = append(append([]string{}, d.Options...), strings.ToUpper(d.Options[0]))
	if Validate(d) == nil {
		t.Fatal("Normalized duplicate option accepted")
	}
}

func TestReplacementFailurePreservesOnlyPreviouslyApprovedQuestions(t *testing.T) {
	for _, failure := range []string{"provider", "incomplete review"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct{ Messages []struct{ Content string } }
				json.NewDecoder(r.Body).Decode(&request)
				var output any
				switch calls {
				case 1:
					output = map[string]any{"exercises": []Draft{mixedFixture("cloze", 1), mixedFixture("cloze", 2)}}
				case 2:
					var input struct{ Questions []Draft }
					json.Unmarshal([]byte(request.Messages[1].Content), &input)
					reviews := []languageVerdict{}
					for _, q := range input.Questions {
						reviews = append(reviews, languageVerdict{q.ID, boolPointer(true), []string{}})
					}
					output = map[string]any{"reviews": reviews}
				case 3:
					if failure == "provider" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					output = map[string]any{"exercises": []Draft{mixedFixture("multiple_choice", 3), mixedFixture("multiple_choice", 4), mixedFixture("multi_select", 5)}}
				default:
					output = map[string]any{"reviews": []languageVerdict{}}
				}
				raw, _ := json.Marshal(output)
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(raw)}}}})
			}))
			defer server.Close()
			api, _ := NewAPI(server.URL, "", "test-model")
			api.client.Transport = server.Client().Transport
			result, err := api.Generate(context.Background(), "Noi suntem acasă.", "lesson", 5)
			if err != nil || len(result.Exercises) != 2 || result.Summary.Note == "" || !result.Summary.RefillAttempted || result.Summary.Shortfall["multiple_choice"] != 2 || result.Summary.Shortfall["multi_select"] != 1 {
				t.Fatalf("Replacement failure lost the approved batch or hid the shortfall: %v", err)
			}
			for _, d := range result.Exercises {
				if d.Kind != "cloze" {
					t.Fatal("An unreviewed replacement was accepted")
				}
			}
		})
	}
}
