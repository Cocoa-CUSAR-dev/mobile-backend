package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- GetTasks: pre-DB branches --------------------------------------------

func TestGetTasks_MissingUserIDPanics(t *testing.T) {
	// The handler does
	//     val, _ := c.Get("userID")
	//     userID := val.(uuid.UUID)
	// which panics if the middleware didn't set userID. We recover and
	// assert 500 so a future hardening (returning 401) is observable.
	h := &FormHandler{}
	r := gin.New()
	r.GET("/tasks", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetTasks(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks?date=2025-03-14", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- SubmitTask: pre-DB branches ------------------------------------------

func TestSubmitTask_MissingUserIDPanics(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/tasks", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.SubmitTask(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{"task_id":"t1","answer":{"q1":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

func TestSubmitTask_InvalidJSON(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.SubmitTask(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if _, ok := body["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

func TestSubmitTask_MissingTaskID(t *testing.T) {
	// task_id has binding:"required" so the empty/missing field must 400
	// before the handler touches the DB.
	h := &FormHandler{}
	r := gin.New()
	r.POST("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.SubmitTask(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{"answer":{"q1":"a"}}`)) // no task_id
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTask_MissingAnswer(t *testing.T) {
	// answer has binding:"required" — empty/missing must 400.
	h := &FormHandler{}
	r := gin.New()
	r.POST("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.SubmitTask(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks",
		strings.NewReader(`{"task_id":"t1"}`)) // no answer
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTask_EmptyBody(t *testing.T) {
	// Empty JSON object fails both required-field checks.
	h := &FormHandler{}
	r := gin.New()
	r.POST("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.SubmitTask(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty body, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- SubmitTaskForUser: pre-DB branches ------------------------------------
//
// Unlike SubmitTask, this handler reads userID from the request body, not
// gin context — so there's no "missing userID panics" case to cover here.
// The chat.conversation existence check (the actual new security boundary)
// touches the DB and isn't covered by these tests, same as dissectAnswer's
// real INSERT isn't — this repo's tests only cover pre-DB branches (see
// TestDissectAnswer_UnsupportedHandlerReturnsError below for the one
// DB-independent case). It's covered by manual verification against a real
// DB instead — see the PR description.

func TestSubmitTaskForUser_InvalidJSON(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/service/tasks", h.SubmitTaskForUser)

	req := httptest.NewRequest(http.MethodPost, "/service/tasks",
		strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTaskForUser_MissingUserID(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/service/tasks", h.SubmitTaskForUser)

	req := httptest.NewRequest(http.MethodPost, "/service/tasks",
		strings.NewReader(`{"task_id":"t1","answer":{"q1":"a"}}`)) // no user_id
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTaskForUser_MissingTaskID(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/service/tasks", h.SubmitTaskForUser)

	req := httptest.NewRequest(http.MethodPost, "/service/tasks",
		strings.NewReader(`{"user_id":"`+uuid.New().String()+`","answer":{"q1":"a"}}`)) // no task_id
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTaskForUser_MissingAnswer(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.POST("/service/tasks", h.SubmitTaskForUser)

	req := httptest.NewRequest(http.MethodPost, "/service/tasks",
		strings.NewReader(`{"user_id":"`+uuid.New().String()+`","task_id":"t1"}`)) // no answer
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTaskForUser_MalformedUserIDRejectedBeforeDB(t *testing.T) {
	// "not-a-uuid" passes binding:"required" (it's a non-empty string) but
	// must fail uuid.Parse before any DB call happens.
	h := &FormHandler{}
	r := gin.New()
	r.POST("/service/tasks", h.SubmitTaskForUser)

	req := httptest.NewRequest(http.MethodPost, "/service/tasks",
		strings.NewReader(`{"user_id":"not-a-uuid","task_id":"t1","answer":{"q1":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed user_id, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSubmitTaskForUserRequest_JSONRoundTrip(t *testing.T) {
	uid := uuid.New().String()
	in := `{"user_id":"` + uid + `","task_id":"task-123","answer":{"q1":"เก็บเกี่ยว"}}`
	var req SubmitTaskForUserRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.UserID != uid {
		t.Errorf("user_id: want %s, got %q", uid, req.UserID)
	}
	if req.TaskID != "task-123" {
		t.Errorf("task_id: want task-123, got %q", req.TaskID)
	}
	if v, ok := req.Answer["q1"].(string); !ok || v != "เก็บเกี่ยว" {
		t.Errorf("answer.q1: want เก็บเกี่ยว, got %v", req.Answer["q1"])
	}
}

// --- GetTaskResponse: pre-DB branches -------------------------------------

func TestGetTaskResponse_MissingUserIDPanics(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.GET("/tasks/:taskId", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetTaskResponse(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks/some-id", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- UpdateTaskResponse: pre-DB branches ----------------------------------

func TestUpdateTaskResponse_MissingUserIDPanics(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.PUT("/tasks", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.UpdateTaskResponse(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/tasks",
		strings.NewReader(`{"task_id":"t1","answer":{"q1":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

func TestUpdateTaskResponse_InvalidJSON(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.PUT("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.UpdateTaskResponse(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/tasks",
		strings.NewReader(`{"task_id":`)) // truncated
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestUpdateTaskResponse_MissingTaskID(t *testing.T) {
	// UpdateTaskResponse re-uses SubmitFormRequest, so missing task_id
	// must 400 before the DB update runs.
	h := &FormHandler{}
	r := gin.New()
	r.PUT("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.UpdateTaskResponse(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/tasks",
		strings.NewReader(`{"answer":{"q1":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestUpdateTaskResponse_EmptyBody(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.PUT("/tasks", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.UpdateTaskResponse(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/tasks", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty body, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- JSON contract tests --------------------------------------------------
//
// These protect against accidental field renames in SubmitFormRequest
// that would silently break the mobile client.

func TestSubmitFormRequest_JSONRoundTrip(t *testing.T) {
	in := `{
		"task_id": "task-123",
		"answer": {
			"q1": "เก็บเกี่ยว",
			"q2": 42,
			"q3": true
		}
	}`
	var req SubmitFormRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.TaskID != "task-123" {
		t.Errorf("task_id: want task-123, got %q", req.TaskID)
	}
	if req.Answer == nil {
		t.Fatal("answer should not be nil after unmarshal")
	}
	if v, ok := req.Answer["q1"].(string); !ok || v != "เก็บเกี่ยว" {
		t.Errorf("answer.q1: want เก็บเกี่ยว, got %v", req.Answer["q1"])
	}
	// Round-trip back to JSON; both fields must serialize.
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"task_id":"task-123"`) {
		t.Errorf("task_id missing from marshalled output: %s", out)
	}
	if !strings.Contains(string(out), `"answer":`) {
		t.Errorf("answer missing from marshalled output: %s", out)
	}
}

// --- Dissection: DB-independent helpers ------------------------------------

func TestFilterKnownColumns_KeepsOnlyRealColumns(t *testing.T) {
	columns := map[string]bool{"harvest_id": true, "harvest_date": true, "quantity_kg": true}
	answer := map[string]interface{}{
		"harvest_date": "2026-01-05",
		"quantity_kg":  12.5,
		"task_id":      "t1",  // injected by SubmitTask, not a real column
		"note_to_self": "n/a", // farmer/client-controlled, not a real column
	}

	got := filterKnownColumns(answer, columns)

	if len(got) != 2 {
		t.Fatalf("want 2 known columns kept, got %d (%v)", len(got), got)
	}
	if got["harvest_date"] != "2026-01-05" || got["quantity_kg"] != 12.5 {
		t.Errorf("unexpected filtered values: %v", got)
	}
	if _, ok := got["task_id"]; ok {
		t.Error("task_id should have been filtered out — not a real column")
	}
	if _, ok := got["note_to_self"]; ok {
		t.Error("note_to_self should have been filtered out — not a real column")
	}
}

func TestFilterKnownColumns_EmptyWhenNoMatch(t *testing.T) {
	columns := map[string]bool{"harvest_id": true}
	answer := map[string]interface{}{"unrelated_field": "value"}

	got := filterKnownColumns(answer, columns)

	if len(got) != 0 {
		t.Errorf("want empty map, got %v", got)
	}
}

func TestStandaloneHandlerTables_MatchesVerifiedHandlers(t *testing.T) {
	// Guards against typos/drift from the verified table in
	// task-dissection-design.md — these 5 handlers have their own
	// generated PK and are safe to dissect generically.
	want := map[string]string{
		"farm_activity":            "agriculture.farm_activity",
		"processing_record":        "processing.processing_record",
		"farm_pest_disease_record": "agriculture.farm_pest_disease_record",
		"harvest":                  "collection.harvest",
		"batch":                    "processing.batch",
	}
	if len(standaloneHandlerTables) != len(want) {
		t.Fatalf("want %d standalone handlers, got %d", len(want), len(standaloneHandlerTables))
	}
	for handler, table := range want {
		if got := standaloneHandlerTables[handler]; got != table {
			t.Errorf("handler %q: want table %q, got %q", handler, table, got)
		}
	}
	// The 5 child-row handlers must NOT be in this map — they need a
	// parent ID the submission doesn't carry (see design doc).
	blocked := []string{
		"farm_activity_fertilizer", "farm_activity_chemical",
		"harvest_grade_detail", "fermentation_batch", "drying_batch",
	}
	for _, handler := range blocked {
		if _, ok := standaloneHandlerTables[handler]; ok {
			t.Errorf("handler %q should not be dissectable yet — needs a parent ID", handler)
		}
	}
}

func TestDissectAnswer_UnsupportedHandlerReturnsError(t *testing.T) {
	err := dissectAnswer(nil, "fermentation_batch", map[string]interface{}{"batch_id": "b1"})
	if err == nil {
		t.Fatal("want error for unsupported handler, got nil")
	}
}

func TestSubmitFormRequest_RequiredFields(t *testing.T) {
	// Each field independently should be rejected when missing.
	// Note: json.Unmarshal alone does NOT enforce binding:"required" —
	// that's a gin tag, applied at ShouldBindJSON time. This test
	// just confirms the body parses without error and the missing
	// field is left at its zero value, so the binding step will
	// (correctly) reject it. The actual 400 response is asserted in
	// TestSubmitTask_MissingTaskID / MissingAnswer / EmptyBody above.
	cases := []struct {
		name        string
		body        string
		wantEmptyID bool
		wantNilAns  bool
	}{
		{"missing task_id", `{"answer":{"q1":"a"}}`, true, false},
		{"missing answer", `{"task_id":"t1"}`, false, true},
		{"both missing", `{}`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req SubmitFormRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if (req.TaskID == "") != tc.wantEmptyID {
				t.Errorf("TaskID empty: want %v, got %v (value=%q)",
					tc.wantEmptyID, req.TaskID == "", req.TaskID)
			}
			if (req.Answer == nil) != tc.wantNilAns {
				t.Errorf("Answer nil: want %v, got %v (value=%v)",
					tc.wantNilAns, req.Answer == nil, req.Answer)
			}
		})
	}
}
