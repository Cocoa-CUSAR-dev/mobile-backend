package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- GetLastAnswer: DB-backed scenarios -------------------------------------
//
// #100 (US2-4): "offer reusing my last submission's answers." The query
// joins form.response to form.task_form on task_log_id/task_id -- exactly
// the join GetTasks already uses, and the one DB-1's misnamed-column note
// (task_log_id actually holds task.task_id, not a real FK) makes worth
// testing against a real Postgres rather than trusting it by inspection.
//
// Reuses openIntegrationTestDB/envOrDefault from auth_handler_db_test.go --
// same package, same RUN_DB_TESTS gate.

const formResponseTestSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS form;

CREATE TABLE IF NOT EXISTS form.task (
	task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	title character varying,
	open_at timestamp without time zone DEFAULT now(),
	close_at timestamp without time zone DEFAULT now() + interval '1 day'
);

CREATE TABLE IF NOT EXISTS form.task_form (
	form_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	task_id uuid NOT NULL REFERENCES form.task(task_id),
	handler character varying NOT NULL
);

CREATE TABLE IF NOT EXISTS form.response (
	response_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	task_log_id uuid NOT NULL,
	task_form_id uuid,
	user_id uuid NOT NULL REFERENCES auth.user_account(user_id),
	submitted_at timestamp without time zone NOT NULL DEFAULT now(),
	answer jsonb,
	status character varying NOT NULL DEFAULT 'COMPLETED'
);
`

func openLastAnswerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openIntegrationTestDB(t) // applies+truncates auth.*, gates on RUN_DB_TESTS
	if err := db.Exec(formResponseTestSchemaDDL).Error; err != nil {
		t.Fatalf("apply form.* test schema: %v", err)
	}
	if err := db.Exec("TRUNCATE form.response, form.task_form, form.task CASCADE").Error; err != nil {
		t.Fatalf("truncate form.* test tables: %v", err)
	}
	return db
}

func seedTaskForm(t *testing.T, db *gorm.DB, handler string) uuid.UUID {
	t.Helper()
	taskID := uuid.New()
	if err := db.Exec("INSERT INTO form.task (task_id, title) VALUES (?, ?)", taskID, "test task").Error; err != nil {
		t.Fatalf("seed form.task: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO form.task_form (task_id, handler) VALUES (?, ?)", taskID, handler,
	).Error; err != nil {
		t.Fatalf("seed form.task_form: %v", err)
	}
	return taskID
}

func seedResponse(
	t *testing.T, db *gorm.DB, userID, taskID uuid.UUID, status string, submittedAt time.Time, answer map[string]interface{},
) {
	t.Helper()
	answerJSON, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO form.response (task_log_id, user_id, status, submitted_at, answer) VALUES (?, ?, ?, ?, ?)",
		taskID, userID, status, submittedAt, string(answerJSON),
	).Error; err != nil {
		t.Fatalf("seed form.response: %v", err)
	}
}

func TestGetLastAnswer_NoPriorSubmission_ReturnsNotFound(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_no_history", "secret123")

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler=farm_activity_fertilizer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetLastAnswer_ReturnsTheMostRecentCompletedOne(t *testing.T) {
	// Three submissions for the same (user, handler), out of chronological
	// insert order on purpose -- the query must pick the one with the
	// latest submitted_at, not the latest-inserted row.
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_history", "secret123")
	taskID := seedTaskForm(t, db, "farm_activity_fertilizer")

	now := time.Now().UTC().Truncate(time.Second)
	seedResponse(t, db, user.UserID, taskID, "COMPLETED", now.Add(-48*time.Hour), map[string]interface{}{"notes": "oldest"})
	seedResponse(t, db, user.UserID, taskID, "COMPLETED", now.Add(-1*time.Hour), map[string]interface{}{"notes": "newest"})
	seedResponse(t, db, user.UserID, taskID, "COMPLETED", now.Add(-24*time.Hour), map[string]interface{}{"notes": "middle"})

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler=farm_activity_fertilizer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var got LastAnswerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Answer["notes"] != "newest" {
		t.Fatalf("want the most recently submitted_at row, got answer=%v", got.Answer)
	}
}

func TestGetLastAnswer_IgnoresAbandonedNonCompletedSubmissions(t *testing.T) {
	// An ACTIVE/PAUSED conversation never reached confirm_conversation, so
	// Go never wrote a form.response row for it at all -- but a CANCELLED
	// or otherwise non-COMPLETED status must never be offered as "your
	// last answer" even if a row does exist.
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_abandoned", "secret123")
	taskID := seedTaskForm(t, db, "farm_activity_fertilizer")

	now := time.Now().UTC().Truncate(time.Second)
	seedResponse(t, db, user.UserID, taskID, "FAILED", now, map[string]interface{}{"notes": "should not surface"})

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler=farm_activity_fertilizer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (non-COMPLETED must not surface), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetLastAnswer_DifferentHandlerIsNotReturned(t *testing.T) {
	// The farmer has a COMPLETED submission, but for a different handler --
	// confirms the join is actually scoping by handler, not just by user.
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_other_handler", "secret123")
	taskID := seedTaskForm(t, db, "harvest")

	seedResponse(t, db, user.UserID, taskID, "COMPLETED", time.Now().UTC(), map[string]interface{}{"notes": "wrong handler"})

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks/last-answer?user_id="+user.UserID.String()+"&handler=farm_activity_fertilizer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (wrong handler), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetLastAnswer_MissingQueryParams_ReturnsBadRequest(t *testing.T) {
	db := openLastAnswerTestDB(t)
	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks/last-answer", h.GetLastAnswer)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks/last-answer?handler=farm_activity_fertilizer", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (missing user_id), got %d", w.Code)
	}
}
