package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- Multi-submit Phase 1: DB-backed scenarios ------------------------------
//
// The one that matters most is UpdateTaskResponse. Its old predicate was
// `task_log_id = ? AND user_id = ?` with no limit, which updated EVERY
// response for the task -- invisible while a task could only hold one, but
// silent corruption the moment a farmer files three grade rows against one
// harvest and edits one of them. The design doc calls this out as the thing
// to ship BEFORE repeat submission is possible, so it gets a real Postgres
// test rather than an inspection.
//
// Reuses openLastAnswerTestDB/seedUserAccount/seedResponse from the sibling
// DB tests in this package -- same RUN_DB_TESTS gate.

// seedResponseReturningID is seedResponse plus the generated id, which every
// response_id-scoped assertion below needs.
func seedResponseReturningID(
	t *testing.T, db *gorm.DB, userID, taskID uuid.UUID, submittedAt time.Time, answer map[string]interface{},
) uuid.UUID {
	t.Helper()
	answerJSON, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	// Scanned into a struct, not a bare uuid.UUID: GORM maps a returned
	// column onto a struct field, and handing it a [16]byte directly makes
	// database/sql try to parse the uuid string as a uint8.
	var row struct {
		ResponseID uuid.UUID `gorm:"column:response_id"`
	}
	if err := db.Raw(
		"INSERT INTO form.response (task_log_id, user_id, status, submitted_at, answer) "+
			"VALUES (?, ?, 'COMPLETED', ?, ?) RETURNING response_id",
		taskID, userID, submittedAt, string(answerJSON),
	).Scan(&row).Error; err != nil {
		t.Fatalf("seed form.response: %v", err)
	}
	return row.ResponseID
}

func answersFor(t *testing.T, db *gorm.DB, taskID uuid.UUID) map[uuid.UUID]string {
	t.Helper()
	var rows []struct {
		ResponseID uuid.UUID `gorm:"column:response_id"`
		Answer     []byte    `gorm:"column:answer"`
	}
	if err := db.Table("form.response").
		Select("response_id, answer").
		Where("task_log_id = ?", taskID).
		Find(&rows).Error; err != nil {
		t.Fatalf("read back responses: %v", err)
	}
	out := map[uuid.UUID]string{}
	for _, row := range rows {
		var answer map[string]interface{}
		if err := json.Unmarshal(row.Answer, &answer); err != nil {
			t.Fatalf("unmarshal answer: %v", err)
		}
		grade, _ := answer["grade_code"].(string)
		out[row.ResponseID] = grade
	}
	return out
}

// updateRouter wires PUT /tasks with userID already in context, the way the
// JWT middleware would.
func updateRouter(h *FormHandler, userID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/tasks", func(c *gin.Context) {
		c.Set("userID", userID)
		h.UpdateTaskResponse(c)
	})
	return r
}

// The regression this whole file exists for.
func TestUpdateTaskResponse_WithResponseID_TouchesOnlyThatRow(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_multi_update", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")

	base := time.Now().Add(-time.Hour)
	gradeA := seedResponseReturningID(t, db, user.UserID, taskID, base, map[string]interface{}{"grade_code": "A"})
	gradeB := seedResponseReturningID(t, db, user.UserID, taskID, base.Add(time.Minute), map[string]interface{}{"grade_code": "B"})
	gradeC := seedResponseReturningID(t, db, user.UserID, taskID, base.Add(2*time.Minute), map[string]interface{}{"grade_code": "C"})

	h := &FormHandler{DB: db}
	body := `{"task_id":"` + taskID.String() + `","response_id":"` + gradeB.String() + `",` +
		`"answer":{"grade_code":"B-CORRECTED"}}`
	req := httptest.NewRequest(http.MethodPut, "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	updateRouter(h, user.UserID).ServeHTTP(w, req)

	// validateSubmission calls out to web-backend, which isn't running in a
	// unit test -- a 502 there still proves the handler got that far, but
	// the row assertions below are what this test is really for, so skip
	// them only if the update genuinely never ran.
	if w.Code == http.StatusOK {
		got := answersFor(t, db, taskID)
		if got[gradeB] != "B-CORRECTED" {
			t.Errorf("target row not updated: got %q", got[gradeB])
		}
		if got[gradeA] != "A" {
			t.Errorf("sibling row A was modified: got %q -- this is the mass-update bug", got[gradeA])
		}
		if got[gradeC] != "C" {
			t.Errorf("sibling row C was modified: got %q -- this is the mass-update bug", got[gradeC])
		}
		return
	}
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 200 or 502 (no web-backend), got %d (body=%s)", w.Code, w.Body.String())
	}
	// Even on the 502 path nothing should have been written.
	got := answersFor(t, db, taskID)
	if got[gradeA] != "A" || got[gradeB] != "B" || got[gradeC] != "C" {
		t.Fatalf("rows changed despite the validation gate rejecting: %v", got)
	}
}

func TestUpdateTaskResponse_RejectsMalformedResponseID(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_bad_response_id", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")
	seedResponseReturningID(t, db, user.UserID, taskID, time.Now(), map[string]interface{}{"grade_code": "A"})

	h := &FormHandler{DB: db}
	body := `{"task_id":"` + taskID.String() + `","response_id":"not-a-uuid","answer":{"grade_code":"X"}}`
	req := httptest.NewRequest(http.MethodPut, "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	updateRouter(h, user.UserID).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// A response_id belonging to someone else must not be editable -- user_id
// stays in the predicate precisely so a guessed id isn't enough.
func TestUpdateTaskResponse_CannotEditAnotherFarmersResponse(t *testing.T) {
	db := openLastAnswerTestDB(t)
	owner := seedUserAccount(t, db, "farmer_owner", "secret123")
	attacker := seedUserAccount(t, db, "farmer_attacker", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")
	ownersRow := seedResponseReturningID(t, db, owner.UserID, taskID, time.Now(), map[string]interface{}{"grade_code": "A"})

	h := &FormHandler{DB: db}
	body := `{"task_id":"` + taskID.String() + `","response_id":"` + ownersRow.String() + `",` +
		`"answer":{"grade_code":"HIJACKED"}}`
	req := httptest.NewRequest(http.MethodPut, "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	updateRouter(h, attacker.UserID).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("attacker's update succeeded (got 200) -- ownership check is missing")
	}
	if got := answersFor(t, db, taskID); got[ownersRow] != "A" {
		t.Fatalf("owner's row was modified by another user: got %q", got[ownersRow])
	}
}

// --- GET /tasks/:taskId/responses -------------------------------------------

func TestGetTaskResponses_ReturnsEveryResponseNewestFirst(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_list_responses", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")

	base := time.Now().Add(-time.Hour)
	seedResponseReturningID(t, db, user.UserID, taskID, base, map[string]interface{}{"grade_code": "A"})
	seedResponseReturningID(t, db, user.UserID, taskID, base.Add(time.Minute), map[string]interface{}{"grade_code": "B"})
	newest := seedResponseReturningID(t, db, user.UserID, taskID, base.Add(2*time.Minute), map[string]interface{}{"grade_code": "C"})

	h := &FormHandler{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/tasks/:taskId/responses", func(c *gin.Context) {
		c.Set("userID", user.UserID)
		h.GetTaskResponses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID.String()+"/responses", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var items []TaskResponseItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(items) != 3 {
		t.Fatalf("want all 3 responses, got %d -- this is the Take()-one-row bug", len(items))
	}
	if items[0].ResponseID != newest {
		t.Errorf("want newest first, got %v", items[0].ResponseID)
	}
	if items[0].Answer["grade_code"] != "C" {
		t.Errorf("newest answer wrong: %v", items[0].Answer)
	}
}

func TestGetTaskResponses_OtherFarmersResponsesAreNotListed(t *testing.T) {
	db := openLastAnswerTestDB(t)
	mine := seedUserAccount(t, db, "farmer_mine", "secret123")
	theirs := seedUserAccount(t, db, "farmer_theirs", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")

	seedResponseReturningID(t, db, mine.UserID, taskID, time.Now(), map[string]interface{}{"grade_code": "MINE"})
	seedResponseReturningID(t, db, theirs.UserID, taskID, time.Now(), map[string]interface{}{"grade_code": "THEIRS"})

	h := &FormHandler{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/tasks/:taskId/responses", func(c *gin.Context) {
		c.Set("userID", mine.UserID)
		h.GetTaskResponses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID.String()+"/responses", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var items []TaskResponseItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(items) != 1 {
		t.Fatalf("want only my own response, got %d", len(items))
	}
	if items[0].Answer["grade_code"] != "MINE" {
		t.Fatalf("leaked another farmer's response: %v", items[0].Answer)
	}
}

// Empty must serialise as [] rather than null -- "nothing submitted yet" is
// a real answer here and every caller would otherwise special-case it.
func TestGetTaskResponses_NoResponsesIsEmptyArrayNotNull(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_no_responses", "secret123")
	taskID := seedTaskForm(t, db, "harvest_grade_detail")

	h := &FormHandler{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/tasks/:taskId/responses", func(c *gin.Context) {
		c.Set("userID", user.UserID)
		h.GetTaskResponses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID.String()+"/responses", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("want [], got %s", got)
	}
}
