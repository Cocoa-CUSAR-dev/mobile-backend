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

// seedTaskFormForUserTest returns (taskID, formID) -- formID is
// form.task_form's own generated PK, asserted against the response's
// task_form_id field (added for docs-and-plan#176's one-tap "open", which
// needs it to start/resume a conversation without an extra round trip).
func seedTaskFormForUserTest(
	t *testing.T, db *gorm.DB, handler string, closeAt time.Time, isMultipleSubmit bool,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	taskID := uuid.New()
	if err := db.Exec(
		"INSERT INTO form.task (task_id, title, close_at) VALUES (?, ?, ?)",
		taskID, "test task", closeAt,
	).Error; err != nil {
		t.Fatalf("seed form.task: %v", err)
	}
	var row struct {
		FormID uuid.UUID `gorm:"column:form_id"`
	}
	if err := db.Raw(
		"INSERT INTO form.task_form (task_id, handler, is_multiple_submit) VALUES (?, ?, ?) "+
			"RETURNING form_id",
		taskID, handler, isMultipleSubmit,
	).Scan(&row).Error; err != nil {
		t.Fatalf("seed form.task_form: %v", err)
	}
	return taskID, row.FormID
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

	notStartedID, notStartedFormID := seedTaskFormForUserTest(t, db, "farm_activity", now.Add(24*time.Hour), false)

	completedID, _ := seedTaskFormForUserTest(t, db, "harvest", now.Add(24*time.Hour), false)
	seedResponse(t, db, user.UserID, completedID, "COMPLETED", now.Add(-1*time.Hour), map[string]interface{}{"ok": true})

	overdueID, _ := seedTaskFormForUserTest(t, db, "processing_record", now.Add(-24*time.Hour), false)

	// Multi-submit with an existing response: must stay IN_PROGRESS, never
	// COMPLETED -- the whole point of #176 listing it as still-pending (a
	// farmer files several grade rows against one harvest).
	multiSubmitID, _ := seedTaskFormForUserTest(t, db, "harvest_grade_detail", now.Add(24*time.Hour), true)
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

	var tasks []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var gotFormID string
	for _, task := range tasks {
		if task["task_id"] == notStartedID.String() {
			gotFormID, _ = task["task_form_id"].(string)
		}
	}
	if gotFormID != notStartedFormID.String() {
		t.Errorf("task_form_id: want %s, got %s", notStartedFormID, gotFormID)
	}
}

// docs-and-plan#254: form.task.close_at is Bangkok wall-clock (web-app sends
// the researcher's datetime-local value as typed), so "is it past close" must
// be asked in Bangkok time. Comparing with a bare NOW() reads close_at as UTC
// and kept a closed task NOT_STARTED for about seven hours.
func TestGetTasksForUser_OverdueFollowsBangkokTime(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_bangkok_clock", "secret123")

	// A fixed +07:00 zone rather than LoadLocation, so the test needs no
	// tzdata on the machine running it. Thailand has no daylight saving.
	bangkok := time.FixedZone("ICT", 7*60*60)
	nowInBangkok := time.Now().In(bangkok)
	// Stored exactly as written, with no zone, the way the create-form page
	// saves it.
	wallClock := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

	seed := func(name string, closeAt time.Time, multiSubmit bool) uuid.UUID {
		taskID, _ := seedTaskFormForUserTest(t, db, name, time.Time{}, multiSubmit)
		if err := db.Exec(
			"UPDATE form.task SET close_at = CAST(? AS timestamp) WHERE task_id = ?",
			wallClock(closeAt), taskID,
		).Error; err != nil {
			t.Fatalf("set close_at: %v", err)
		}
		return taskID
	}

	// Closed two hours ago in Bangkok -- but still five hours "ahead" if
	// close_at were read as UTC, which is exactly the old bug.
	closedID := seed("closed_two_hours_ago", nowInBangkok.Add(-2*time.Hour), false)
	closedMultiID := seed("closed_multi_submit", nowInBangkok.Add(-2*time.Hour), true)
	// Closes later today in Bangkok: not overdue either way.
	openID := seed("closes_later_today", nowInBangkok.Add(2*time.Hour), false)

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
	for taskID, want := range map[string]string{
		closedID.String():      "OVERDUE",
		closedMultiID.String(): "OVERDUE",
		openID.String():        "NOT_STARTED",
	} {
		if got := statuses[taskID]; got != want {
			t.Errorf("task %s: want %s, got %q", taskID, want, got)
		}
	}
}
