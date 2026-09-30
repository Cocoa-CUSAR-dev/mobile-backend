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

// --- GetTasksForUser: DB-backed scenarios -----------------------------------
//
// docs-and-plan#176: the chatbot's LIFF to-do list needs "due status" per
// task, which is exactly queryTasksForUser's status computation (shared with
// GetTasks) -- these tests exercise that computation through the service-key
// route rather than trusting it by inspection, same reasoning as the
// GetLastAnswer DB tests above.
//
// Reuses openLastAnswerTestDB/seedUserAccount from the sibling DB tests in
// this package -- same RUN_DB_TESTS gate.

func seedTaskFormForUserTest(
	t *testing.T, db *gorm.DB, handler string, closeAt time.Time, isMultipleSubmit bool,
) uuid.UUID {
	t.Helper()
	taskID := uuid.New()
	if err := db.Exec(
		"INSERT INTO form.task (task_id, title, close_at) VALUES (?, ?, ?)",
		taskID, "test task", closeAt,
	).Error; err != nil {
		t.Fatalf("seed form.task: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO form.task_form (task_id, handler, is_multiple_submit) VALUES (?, ?, ?)",
		taskID, handler, isMultipleSubmit,
	).Error; err != nil {
		t.Fatalf("seed form.task_form: %v", err)
	}
	return taskID
}

func taskStatuses(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var tasks []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	byTaskID := make(map[string]string, len(tasks))
	for _, task := range tasks {
		taskID, _ := task["task_id"].(string)
		status, _ := task["status"].(string)
		byTaskID[taskID] = status
	}
	return byTaskID
}

func TestGetTasksForUser_MissingUserID_ReturnsBadRequest(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.GET("/service/tasks", h.GetTasksForUser)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTasksForUser_InvalidUserID_ReturnsBadRequest(t *testing.T) {
	h := &FormHandler{}
	r := gin.New()
	r.GET("/service/tasks", h.GetTasksForUser)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks?user_id=not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTasksForUser_StatusMatchesEachTaskShape(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_todo_list", "secret123")

	now := time.Now().UTC().Truncate(time.Second)

	notStartedID := seedTaskFormForUserTest(t, db, "farm_activity", now.Add(24*time.Hour), false)

	completedID := seedTaskFormForUserTest(t, db, "harvest", now.Add(24*time.Hour), false)
	seedResponse(t, db, user.UserID, completedID, "COMPLETED", now.Add(-1*time.Hour), map[string]interface{}{"ok": true})

	overdueID := seedTaskFormForUserTest(t, db, "processing_record", now.Add(-24*time.Hour), false)

	// Multi-submit with an existing response: must stay IN_PROGRESS, never
	// COMPLETED -- the whole point of #176 listing it as still-pending (a
	// farmer files several grade rows against one harvest).
	multiSubmitID := seedTaskFormForUserTest(t, db, "harvest_grade_detail", now.Add(24*time.Hour), true)
	seedResponse(t, db, user.UserID, multiSubmitID, "COMPLETED", now.Add(-1*time.Hour), map[string]interface{}{"grade": "A"})

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks", h.GetTasksForUser)

	req := httptest.NewRequest(http.MethodGet, "/service/tasks?user_id="+user.UserID.String()+"&page_size=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	statuses := taskStatuses(t, w)
	cases := map[string]string{
		notStartedID.String():  "NOT_STARTED",
		completedID.String():   "COMPLETED",
		overdueID.String():     "OVERDUE",
		multiSubmitID.String(): "IN_PROGRESS",
	}
	for taskID, want := range cases {
		got, ok := statuses[taskID]
		if !ok {
			t.Errorf("task %s missing from response entirely", taskID)
			continue
		}
		if got != want {
			t.Errorf("task %s: want status %s, got %s", taskID, want, got)
		}
	}
}
