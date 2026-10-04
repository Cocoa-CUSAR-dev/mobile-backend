package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-server-mobile/internal/validation"

	"github.com/gin-gonic/gin"
)

// #106 (US2-5): "TEST - Autofill parity e2e (chat vs static form)."
//
// When this test was written no static-form screen called these endpoints
// (the mobile app now has its own, GET /tasks/:taskId/autofill -- the real
// chat-vs-app comparison is the TestAutofillParity_* tests further down).
// So a literal chat-vs-UI comparison didn't exist to test. What #105/US2-5 actually promises is narrower and *is*
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

// --- #106 for real: chatbot path vs the app's GET /tasks/:taskId/autofill ---
//
// The test above could only pass "vacuously": until the app had its own
// endpoint there was one implementation and no second channel. Now there
// are two real paths, and these tests compare what each channel would
// actually OFFER a farmer for the same last submission:
//
//	(a) the chatbot: /service/tasks/last-answer -> /service/autofill/sanitize,
//	    sending the question list the chatbot really sends (see
//	    chatbotQuestions), then keeping only the fields it has a question
//	    for (reuse.py's build_answer_rows/format_autofill_preview ignore the
//	    rest);
//	(b) the app: GET /tasks/:taskId/autofill with a farmer JWT, keeping only
//	    the fields on the form the app renders.
//
// Why "offered" and not the raw sanitized maps: the chatbot drops GEODATA and
// "upload" questions BEFORE calling sanitize (service.py's _is_supported), so
// the sanitizer never learns those fields are GEODATA on that path and a
// location survives it there -- the chatbot then ignores it because it has
// no question for it. The raw maps can therefore legitimately differ while
// the farmer sees exactly the same offer, and the offer is what US2-5
// promises is identical.

// chatbotSupportedInputTypes mirrors chatbot src/conversation/service.py's
// _SUPPORTED_INPUT_TYPES. If the chatbot's list changes, change this too.
var chatbotSupportedInputTypes = map[string]bool{
	"VARCHAR": true, "OPTION": true, "BOOLEAN": true,
	"FLOAT": true, "INT": true, "DATE": true, "DATETIME": true,
}

// chatbotQuestions is the question list the chatbot sends to
// /service/autofill/sanitize: questions_from_form's _is_supported filter --
// a supported input type, and not the VARCHAR "upload" field.
func chatbotQuestions(all []validation.Question) []validation.Question {
	var kept []validation.Question
	for _, q := range all {
		inputType := strings.ToUpper(q.InputType)
		if !chatbotSupportedInputTypes[inputType] {
			continue
		}
		if inputType == "VARCHAR" && q.FieldName == "upload" {
			continue
		}
		kept = append(kept, q)
	}
	return kept
}

// offeredFields keeps only the fields a channel has a question for -- what
// it would actually show in its preview and prefill.
func offeredFields(answer map[string]interface{}, questions []validation.Question) map[string]interface{} {
	onForm := map[string]bool{}
	for _, q := range questions {
		onForm[q.FieldName] = true
	}
	offered := map[string]interface{}{}
	for field, value := range answer {
		if onForm[field] {
			offered[field] = value
		}
	}
	return offered
}

// paritySchemaJSON is the CURRENT form both channels see: free text, an
// OPTION whose only surviving choice is plot-a, a BOOLEAN, a GEODATA
// location and the upload field.
const paritySchemaJSON = `` +
	`{"fieldName":"description","inputType":"VARCHAR","choices":[]},` +
	`{"fieldName":"plot_id","inputType":"OPTION","choices":[{"id":"plot-a","name":"แปลง A"}]},` +
	`{"fieldName":"is_quality_damage","inputType":"BOOLEAN","choices":[]},` +
	`{"fieldName":"geo_location","inputType":"GEODATA","choices":[]},` +
	`{"fieldName":"upload","inputType":"VARCHAR","choices":[]}`

func parityQuestions(t *testing.T) []validation.Question {
	t.Helper()
	var questions []validation.Question
	if err := json.Unmarshal([]byte("["+paritySchemaJSON+"]"), &questions); err != nil {
		t.Fatalf("decode parity schema: %v", err)
	}
	return questions
}

// offersFromBothChannels seeds one COMPLETED submission of `handler` and
// returns what the chatbot path (a) and the app path (b) would offer for a
// NEW task of the same handler. An empty map means "no offer".
func offersFromBothChannels(
	t *testing.T, handler string, rawAnswer map[string]interface{},
) (chat, app map[string]interface{}) {
	t.Helper()
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, paritySchemaJSON)
	user := seedUserAccount(t, db, "farmer_parity", "secret123")
	pastTask := seedTaskFormFor(t, db, handler, false)
	newTask := seedTaskFormFor(t, db, handler, false)
	seedResponse(t, db, user.UserID, pastTask, "COMPLETED", time.Now().UTC(), rawAnswer)

	h := &FormHandler{DB: db}
	questions := parityQuestions(t)

	// (a) The chatbot. It never even asks for a last answer on a
	// parent-picker handler (router.py resolves the parent picker first and
	// skips the offer), so that case is "no offer" by construction.
	chat = map[string]interface{}{}
	if !parentPickerHandlers[handler] {
		r := gin.New()
		r.GET("/service/tasks/last-answer", h.GetLastAnswer)
		r.POST("/service/autofill/sanitize", SanitizeAutofill)

		lastW := httptest.NewRecorder()
		r.ServeHTTP(lastW, httptest.NewRequest(http.MethodGet,
			"/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler="+handler, nil))
		if lastW.Code == http.StatusOK {
			var last LastAnswerResponse
			if err := json.Unmarshal(lastW.Body.Bytes(), &last); err != nil {
				t.Fatalf("decode last-answer: %v", err)
			}
			sent := chatbotQuestions(questions)
			body, err := json.Marshal(SanitizeAutofillRequest{Answer: last.Answer, Questions: sent})
			if err != nil {
				t.Fatalf("marshal sanitize request: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/service/autofill/sanitize", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			sanW := httptest.NewRecorder()
			r.ServeHTTP(sanW, req)
			if sanW.Code != http.StatusOK {
				t.Fatalf("sanitize: want 200, got %d (body=%s)", sanW.Code, sanW.Body.String())
			}
			var sanitized SanitizeAutofillResponse
			if err := json.Unmarshal(sanW.Body.Bytes(), &sanitized); err != nil {
				t.Fatalf("decode sanitize: %v", err)
			}
			chat = offeredFields(sanitized.Answer, sent)
		}
	}

	// (b) The app.
	app = map[string]interface{}{}
	w := getAutofill(t, autofillRouter(h), newTask, farmerToken(t, user.UserID), "")
	switch w.Code {
	case http.StatusOK:
		app = offeredFields(decodeAutofill(t, w).Answer, questions)
	case http.StatusNoContent:
		// no offer
	default:
		t.Fatalf("app autofill: want 200 or 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	return chat, app
}

func assertSameOffer(t *testing.T, chat, app, want map[string]interface{}) {
	t.Helper()
	chatJSON, _ := json.Marshal(chat)
	appJSON, _ := json.Marshal(app)
	if string(chatJSON) != string(appJSON) {
		t.Fatalf("the two channels would offer different things:\nchatbot: %s\napp:     %s", chatJSON, appJSON)
	}
	wantJSON, _ := json.Marshal(want)
	if string(chatJSON) != string(wantJSON) {
		t.Fatalf("both channels agree, but on the wrong offer:\nwant: %s\ngot:  %s", wantJSON, chatJSON)
	}
}

func TestAutofillParity_SameOfferForStillValidAnswers(t *testing.T) {
	chat, app := offersFromBothChannels(t, "farm_activity", map[string]interface{}{
		"description":       "ฉีดพ่นรอบโคน",
		"plot_id":           "plot-a",
		"is_quality_damage": "false",
	})
	assertSameOffer(t, chat, app, map[string]interface{}{
		"description":       "ฉีดพ่นรอบโคน",
		"plot_id":           "plot-a",
		"is_quality_damage": "false",
	})
}

func TestAutofillParity_StaleOptionDroppedInBoth(t *testing.T) {
	chat, app := offersFromBothChannels(t, "farm_activity", map[string]interface{}{
		"description": "kept",
		"plot_id":     "plot-deleted-since",
	})
	assertSameOffer(t, chat, app, map[string]interface{}{"description": "kept"})
}

func TestAutofillParity_StaleParentIdDroppedInBoth(t *testing.T) {
	chat, app := offersFromBothChannels(t, "farm_activity", map[string]interface{}{
		"description":      "kept",
		"farm_activity_id": "an-old-parent-row",
		"task_id":          "the-old-task",
	})
	assertSameOffer(t, chat, app, map[string]interface{}{"description": "kept"})
}

func TestAutofillParity_LocationAndPhotoNeverOfferedInEither(t *testing.T) {
	// The case that made raw-map comparison the wrong test: on the chatbot
	// path the location SURVIVES the sanitizer (its question is never sent),
	// but neither channel offers it.
	chat, app := offersFromBothChannels(t, "farm_activity", map[string]interface{}{
		"description":  "kept",
		"geo_location": "13.7563,100.5018",
		"upload":       "https://storage.example/last-time.jpg",
	})
	assertSameOffer(t, chat, app, map[string]interface{}{"description": "kept"})
}

func TestAutofillParity_ParentPickerHandlerOffersNothingInEither(t *testing.T) {
	chat, app := offersFromBothChannels(t, "farm_activity_fertilizer", map[string]interface{}{
		"description": "would be offered on any other form",
	})
	assertSameOffer(t, chat, app, map[string]interface{}{})
}

func TestAutofillParity_ParentPickerHandlerListMatchesTheChatbots(t *testing.T) {
	// chatbot src/line/parent_picker.py's _PARENT_KIND_BY_HANDLER keys. The
	// two lists live in different repos, so this pins the Go copy down; if
	// the chatbot gains or loses a parent-picker handler, both must change.
	want := []string{
		"farm_activity_fertilizer", "farm_activity_chemical",
		"harvest_grade_detail", "fermentation_batch", "drying_batch",
	}
	if len(parentPickerHandlers) != len(want) {
		t.Fatalf("want %d parent-picker handlers, got %d: %v", len(want), len(parentPickerHandlers), parentPickerHandlers)
	}
	for _, handler := range want {
		if !parentPickerHandlers[handler] {
			t.Fatalf("parentPickerHandlers is missing %q", handler)
		}
	}
}
