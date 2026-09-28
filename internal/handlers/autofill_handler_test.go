package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// These bodies are written by hand, byte-for-byte matching what chatbot's
// src/conversation/reuse.py actually sends (fieldName/inputType/choices,
// choices as {id, name}) -- not built via SanitizeAutofillRequest, so this
// test would actually fail if the JSON tags on validation.Question/Choice
// ever drift from what the Python side is hardcoded to send.

func TestSanitizeAutofill_MatchesChatbotsRealPayloadShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/service/autofill/sanitize", SanitizeAutofill)

	body := `{
		"answer": {
			"task_id": "t-1",
			"farm_activity_id": "fa-1",
			"note": "sprayed at dawn",
			"fertilizer_id": "stale-id"
		},
		"questions": [
			{"fieldName": "note", "inputType": "VARCHAR", "choices": []},
			{"fieldName": "fertilizer_id", "inputType": "OPTION", "choices": [
				{"id": "current-id", "name": "current"}
			]}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/service/autofill/sanitize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var resp SanitizeAutofillResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not decode response: %v (body=%s)", err, w.Body.String())
	}

	want := map[string]interface{}{"note": "sprayed at dawn"}
	if len(resp.Answer) != len(want) || resp.Answer["note"] != want["note"] {
		t.Fatalf("got %v, want %v", resp.Answer, want)
	}
}

func TestSanitizeAutofill_InvalidJSONReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/service/autofill/sanitize", SanitizeAutofill)

	req := httptest.NewRequest(http.MethodPost, "/service/autofill/sanitize", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSanitizeAutofill_EmptyAnswerReturnsEmptyObjectNotNull(t *testing.T) {
	// A farmer whose entire last answer gets filtered out should still get
	// back `{"answer": {}}`, not `{"answer": null}` -- chatbot's own
	// fetch_sanitized_autofill treats a non-dict `answer` as an upstream
	// error (see src/tasks/client.py), so a nil map serializing to `null`
	// would incorrectly look like a broken response.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/service/autofill/sanitize", SanitizeAutofill)

	body := `{"answer": {"task_id": "t-1"}, "questions": []}`
	req := httptest.NewRequest(http.MethodPost, "/service/autofill/sanitize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"answer":{}`) {
		t.Fatalf(`want body to contain "answer":{}, got %s`, w.Body.String())
	}
}
