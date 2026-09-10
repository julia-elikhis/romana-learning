package app

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

func TestAIGenerationSavesOnlyAfterLanguageApproval(t *testing.T) {
	db := isolatedDB(t)
	approved := false
	calls := 0
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var output any
		if calls%2 == 1 {
			output = map[string]any{"exercises": []materials.Draft{{Kind: "cloze", Prompt: "Eu ____ acasă.", Answers: []string{"sunt"}, Explanation: "Eu sunt means I am.", SourceQuote: "Eu sunt acasă.", Skill: "grammar", Target: "a fi / eu", Difficulty: "easy"}}}
		} else {
			var request struct {
				Messages []struct{ Content string } `json:"messages"`
			}
			var input struct{ Questions []materials.Draft }
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 || json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input.Questions) != 1 {
				t.Error("Missing second language-review request")
				w.WriteHeader(400)
				return
			}
			issues := []string{}
			if !approved {
				issues = []string{"grammar"}
			}
			output = map[string]any{"reviews": []any{map[string]any{"id": input.Questions[0].ID, "approved": approved, "issues": issues}}}
		}
		content, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(content)}}}})
	}))
	defer provider.Close()
	// Trust only this fixture's certificate. The production adapter retains TLS validation.
	roots := x509.NewCertPool()
	roots.AddCert(provider.Certificate())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	api, err := materials.NewAPI(provider.URL, "", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	handler := authenticatedHandler(t, db, Server{DB: db, Generator: api}.Routes())
	id, _ := questionFixtures(t, db, 0, "draft")
	if _, err := db.Exec(`UPDATE materials SET reviewed=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"count": 1, "mode": "api"}
	questionRequest(t, handler, "POST", "/api/materials/"+id+"/generate", payload, 422)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM exercises WHERE material_id=$1`, id).Scan(&count); err != nil || count != 0 || calls != 4 {
		t.Fatal("Rejected language review persisted questions or skipped its separate request")
	}
	approved = true
	response := questionRequest(t, handler, "POST", "/api/materials/"+id+"/generate", payload, 201)
	var result struct {
		Count            int
		LanguageReviewed bool
		Mix              materials.GenerationSummary
	}
	if json.Unmarshal(response, &result) != nil || result.Count != 1 || !result.LanguageReviewed || calls != 6 {
		t.Fatal("Approved generation did not report its completed language review")
	}
	var detail struct {
		GenerationSummary materials.GenerationSummary
		Exercises         []materials.Draft
	}
	if json.Unmarshal(questionRequest(t, handler, "GET", "/api/materials/"+id, nil, 200), &detail) != nil || !reflect.DeepEqual(detail.GenerationSummary, result.Mix) || !reflect.DeepEqual(result.Mix.Created, materials.PracticeMix(1)) {
		t.Fatal("Saved generation mix does not match the actual created formats")
	}
	if len(detail.Exercises) != 1 || detail.Exercises[0].Skill != "grammar" || detail.Exercises[0].Target != "a fi / eu" || detail.Exercises[0].Difficulty != "easy" {
		t.Fatal("Generated learning focus was not preserved")
	}
	var status, version string
	if err := db.QueryRow(`SELECT status,generator_version FROM exercises WHERE material_id=$1`, id).Scan(&status, &version); err != nil || status != "draft" || version != materials.APIGeneratorVersion {
		t.Fatal("Reviewed questions must still be unpublished drafts")
	}
	if string(questionRequest(t, handler, "GET", "/api/exercises?materialId="+id, nil, 200)) != "[]\n" {
		t.Fatal("Language approval automatically published questions")
	}
}
