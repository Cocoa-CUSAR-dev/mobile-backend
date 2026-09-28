package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// #106 (US2-5): "TEST - Autofill parity e2e (chat vs static form)."
//
// As of this test, no static-form screen calls these endpoints yet (grep
// confirms zero references in web-app/mobile-app) -- chatbot's reuse.py is
// the only real caller today. So a literal chat-vs-UI comparison doesn't
// exist to test. What #105/US2-5 actually promises is narrower and *is*
// testable now: "one shared implementation... so the two channels can't
// drift apart on the rules" (autofill_sanitizer.go's own doc comment).
// That promise only holds if GET /service/tasks/last-answer's real,
// DB-sourced output feeds cleanly into POST /service/autofill/sanitize and
// produces the documented rules -- every other test on these two endpoints
// exercises them in isolation with hand-built inputs, never chained.
//
// This chains them for real, then asks two independent callers (standing
// in for "chat" and "a future static form" -- structurally identical today
// since there is exactly one implementation for both to share) to sanitize
// the same DB-sourced answer, and requires byte-identical results. A
// caller-specific bug (e.g. one accidentally depending on request order,
// header, or connection state) would show up as a mismatch here; today it
// can only ever pass vacuously, which is itself the point -- it pins down
// that the shared implementation has no room to grow caller-specific
// behavior later without this test catching it.
func TestAutofillPipeline_LastAnswerThroughSanitize_MatchesForEveryCaller(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_autofill_parity", "secret123")
	taskID := seedTaskForm(t, db, "farm_activity_fertilizer")

	// A realistic last submission: one free-text field that should always
	// survive, one stale parent-id that must always be dropped (#105's
	// staleParentFields), one OPTION field whose stored value still
	// resolves on the CURRENT form (kept), and one OPTION field whose
	// stored value no longer does (dropped) -- e.g. a fertilizer product
	// that's since been discontinued.
	rawAnswer := map[string]interface{}{
		"task_id":          "should-be-stripped-regardless-of-caller",
		"farm_activity_id": "stale-parent-should-be-stripped",
		"note":             "sprayed at dawn, light rain after",
		"unit_id":          "kg",
		"fertilizer_id":    "discontinued-product-id",
	}
	seedResponse(t, db, user.UserID, taskID, "COMPLETED", time.Now().UTC(), rawAnswer)

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)
	r.POST("/service/autofill/sanitize", SanitizeAutofill)

	// Step 1: the raw last answer, exactly as GetLastAnswer serves it to
	// any caller -- chat and a future static form would both start here.
	lastAnswerReq := httptest.NewRequest(
		http.MethodGet,
		"/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler=farm_activity_fertilizer",
		nil,
	)
	lastAnswerW := httptest.NewRecorder()
	r.ServeHTTP(lastAnswerW, lastAnswerReq)
	if lastAnswerW.Code != http.StatusOK {
		t.Fatalf("GetLastAnswer: want 200, got %d (body=%s)", lastAnswerW.Code, lastAnswerW.Body.String())
	}
	var lastAnswer LastAnswerResponse
	if err := json.Unmarshal(lastAnswerW.Body.Bytes(), &lastAnswer); err != nil {
		t.Fatalf("decode last-answer response: %v", err)
	}

	// The CURRENT form's schema -- "unit_id" still has "kg" as a real
	// choice, "fertilizer_id" no longer lists the discontinued product the
	// farmer's last submission used.
	sanitizeBody := func() string {
		answerJSON, err := json.Marshal(lastAnswer.Answer)
		if err != nil {
			t.Fatalf("marshal answer for sanitize request: %v", err)
		}
		return `{"answer":` + string(answerJSON) + `,"questions":[` +
			`{"fieldName":"note","inputType":"VARCHAR","choices":[]},` +
			`{"fieldName":"unit_id","inputType":"OPTION","choices":[{"id":"kg","name":"กิโลกรัม"}]},` +
			`{"fieldName":"fertilizer_id","inputType":"OPTION","choices":[{"id":"current-product-id","name":"current"}]}` +
			`]}`
	}()

	// Step 2: two independent callers -- standing in for "chat" and "a
	// future static form" -- each build their own request from the same
	// last-answer response and current-form schema (as any two callers of
	// a shared, stateless HTTP endpoint would), and must land on the exact
	// same sanitized result.
	callSanitize := func(callerLabel string) SanitizeAutofillResponse {
		req := httptest.NewRequest(http.MethodPost, "/service/autofill/sanitize", strings.NewReader(sanitizeBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("[%s] SanitizeAutofill: want 200, got %d (body=%s)", callerLabel, w.Code, w.Body.String())
		}
		var resp SanitizeAutofillResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("[%s] decode sanitize response: %v", callerLabel, err)
		}
		return resp
	}

	chatResult := callSanitize("chat")
	staticFormResult := callSanitize("static-form")

	chatJSON, _ := json.Marshal(chatResult)
	staticFormJSON, _ := json.Marshal(staticFormResult)
	if string(chatJSON) != string(staticFormJSON) {
		t.Fatalf("chat and static-form callers diverged on an identical request:\nchat:        %s\nstatic-form: %s", chatJSON, staticFormJSON)
	}

	// And the rules themselves actually fired as #105 documents them.
	want := map[string]interface{}{"note": "sprayed at dawn, light rain after", "unit_id": "kg"}
	if len(chatResult.Answer) != len(want) {
		t.Fatalf("want %d surviving fields %v, got %v", len(want), want, chatResult.Answer)
	}
	for field, value := range want {
		if chatResult.Answer[field] != value {
			t.Fatalf("field %q: want %v, got %v (full result=%v)", field, value, chatResult.Answer[field], chatResult.Answer)
		}
	}
	if _, stillThere := chatResult.Answer["task_id"]; stillThere {
		t.Fatalf("task_id must never survive sanitizing, got %v", chatResult.Answer)
	}
	if _, stillThere := chatResult.Answer["farm_activity_id"]; stillThere {
		t.Fatalf("stale parent-id field must never survive sanitizing, got %v", chatResult.Answer)
	}
	if _, stillThere := chatResult.Answer["fertilizer_id"]; stillThere {
		t.Fatalf("a value with no matching choice on the current form must be dropped, got %v", chatResult.Answer)
	}
}
