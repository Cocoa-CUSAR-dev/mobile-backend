package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-server-mobile/internal/middleware"
	"go-server-mobile/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- GET /tasks/:taskId/autofill: DB-backed scenarios ------------------------
//
// US2-5 (docs-and-plan#98). Wired through the REAL JwtAuthMiddleware rather
// than a c.Set("userID") shortcut, because two of the things being tested
// are about identity: no token means no answer, and the user is the token's,
// never a query parameter's. Kotlin is faked with an httptest server via
// WEB_BACKEND_URL, the same way form_schema_client_test.go does it.

// autofillSchemaServer serves one fixed schema for any form id.
func autofillSchemaServer(t *testing.T, questionsJSON string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"value":{"sections":[{"questions":[%s]}]},"error":null}`, questionsJSON)
	}))
	t.Cleanup(server.Close)
	withFormSchemaEnv(t, server.URL, "test-service-key")
}

// seedTaskFormFor creates a task + task_form and returns the task id. Unlike
// seedTaskForm it can mark the form multiple-submit, which the offer gate
// depends on.
func seedTaskFormFor(t *testing.T, db *gorm.DB, handler string, multipleSubmit bool) uuid.UUID {
	t.Helper()
	taskID := uuid.New()
	if err := db.Exec("INSERT INTO form.task (task_id, title) VALUES (?, ?)", taskID, "test task").Error; err != nil {
		t.Fatalf("seed form.task: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO form.task_form (task_id, handler, is_multiple_submit) VALUES (?, ?, ?)",
		taskID, handler, multipleSubmit,
	).Error; err != nil {
		t.Fatalf("seed form.task_form: %v", err)
	}
	return taskID
}

func autofillRouter(h *FormHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/tasks/:taskId/autofill", middleware.JwtAuthMiddleware(), h.GetTaskAutofill)
	return r
}

func farmerToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	token, _, err := services.GenerateToken(userID, "farmer", []string{"farmer"})
	if err != nil {
		t.Fatalf("mint farmer token: %v", err)
	}
	return token
}

func getAutofill(t *testing.T, r *gin.Engine, taskID uuid.UUID, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/tasks/" + taskID.String() + "/autofill" + query
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeAutofill(t *testing.T, w *httptest.ResponseRecorder) TaskAutofillResponse {
	t.Helper()
	var resp TaskAutofillResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode autofill response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

const farmActivitySchemaJSON = `` +
	`{"fieldName":"description","inputType":"VARCHAR","choices":[]},` +
	`{"fieldName":"plot_id","inputType":"OPTION","choices":[{"id":"plot-a","name":"แปลง A"}]},` +
	`{"fieldName":"geo_location","inputType":"GEODATA","choices":[]},` +
	`{"fieldName":"upload","inputType":"VARCHAR","choices":[]}`

func TestGetTaskAutofill_NoTokenIs401(t *testing.T) {
	db := openLastAnswerTestDB(t)
	taskID := seedTaskFormFor(t, db, "farm_activity", false)

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), taskID, "", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without a JWT, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_OffersTheFarmersLastAnswerFromAnotherTask(t *testing.T) {
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_autofill_offer", "secret123")
	previousTask := seedTaskFormFor(t, db, "farm_activity", false)
	newTask := seedTaskFormFor(t, db, "farm_activity", false)
	submittedAt := time.Date(2026, 9, 30, 8, 12, 0, 0, time.UTC)
	seedResponse(t, db, user.UserID, previousTask, "COMPLETED", submittedAt, map[string]interface{}{
		"description": "ฉีดพ่นรอบโคน",
		"plot_id":     "plot-a",
	})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeAutofill(t, w)
	if !resp.SubmittedAt.Equal(submittedAt) {
		t.Errorf("submitted_at: want %v, got %v", submittedAt, resp.SubmittedAt)
	}
	assertAutofillAnswer(t, resp.Answer, map[string]interface{}{
		"description": "ฉีดพ่นรอบโคน",
		"plot_id":     "plot-a",
	})
}

func TestGetTaskAutofill_UserComesFromTheJwtNotAQueryParam(t *testing.T) {
	// The /service/ variant takes ?user_id= because its caller is a trusted
	// service. Here the same parameter must be ignored outright, or any
	// farmer could read any other farmer's last submission.
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	me := seedUserAccount(t, db, "farmer_me", "secret123")
	someoneElse := seedUserAccount(t, db, "farmer_someone_else", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity", false)
	newTask := seedTaskFormFor(t, db, "farm_activity", false)
	seedResponse(t, db, me.UserID, pastTask, "COMPLETED", time.Now().UTC().Add(-time.Hour),
		map[string]interface{}{"description": "mine"})
	seedResponse(t, db, someoneElse.UserID, pastTask, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"description": "not mine"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask,
		farmerToken(t, me.UserID), "?user_id="+someoneElse.UserID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeAutofill(t, w).Answer["description"]; got != "mine" {
		t.Fatalf("user_id query param must be ignored: want the token holder's answer, got %v", got)
	}
}

func TestGetTaskAutofill_NoHistoryIs204(t *testing.T) {
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_no_history", "secret123")
	newTask := seedTaskFormFor(t, db, "farm_activity", false)

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_ParentPickerHandlerIs204(t *testing.T) {
	// The chatbot never offers for these 5; neither may the app.
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, `{"fieldName":"note","inputType":"VARCHAR","choices":[]}`)
	user := seedUserAccount(t, db, "farmer_parent_picker", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity_fertilizer", false)
	newTask := seedTaskFormFor(t, db, "farm_activity_fertilizer", false)
	seedResponse(t, db, user.UserID, pastTask, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"note": "would otherwise be offered"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204 for a parent-picker handler, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_SingleSubmitTaskAlreadyAnsweredIs204(t *testing.T) {
	// Opening it again is EDITING (GetTaskResponse prefills it), not a new
	// submission -- autofill doesn't apply.
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_already_answered", "secret123")
	task := seedTaskFormFor(t, db, "farm_activity", false)
	seedResponse(t, db, user.UserID, task, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"description": "already answered"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), task, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204 in edit mode, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_SomeoneElsesAnswerOnThisTaskDoesNotCountAsMine(t *testing.T) {
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	me := seedUserAccount(t, db, "farmer_me_2", "secret123")
	other := seedUserAccount(t, db, "farmer_other_2", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity", false)
	sharedTask := seedTaskFormFor(t, db, "farm_activity", false)
	seedResponse(t, db, me.UserID, pastTask, "COMPLETED", time.Now().UTC().Add(-time.Hour),
		map[string]interface{}{"description": "mine"})
	seedResponse(t, db, other.UserID, sharedTask, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"description": "theirs"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), sharedTask, farmerToken(t, me.UserID), "")

	if w.Code != http.StatusOK {
		t.Fatalf("another farmer's answer must not put me in edit mode: want 200, got %d", w.Code)
	}
}

func TestGetTaskAutofill_MultiSubmitFirstRowIsOffered(t *testing.T) {
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_multi_first", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity", true)
	newTask := seedTaskFormFor(t, db, "farm_activity", true)
	seedResponse(t, db, user.UserID, pastTask, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"description": "last time"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 on a multi-submit task's first row, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_MultiSubmitTaskWithARowIsStillOffered(t *testing.T) {
	// The server doesn't treat a multi-submit task with rows as edit mode --
	// opening it again starts a NEW row. (The app only asks on the first row;
	// that decision is the app's, see the design doc §3.2.)
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_multi_later", "secret123")
	task := seedTaskFormFor(t, db, "farm_activity", true)
	seedResponse(t, db, user.UserID, task, "COMPLETED", time.Now().UTC(),
		map[string]interface{}{"description": "earlier row"})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), task, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_EverythingSanitizedAwayIs204(t *testing.T) {
	// A stale parent id, a location, a photo and a deleted choice: nothing
	// survives, and an offer of nothing is worse than no offer.
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_all_stale", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity", false)
	newTask := seedTaskFormFor(t, db, "farm_activity", false)
	seedResponse(t, db, user.UserID, pastTask, "COMPLETED", time.Now().UTC(), map[string]interface{}{
		"task_id":          pastTask.String(),
		"farm_activity_id": "stale-parent",
		"geo_location":     "13.75,100.50",
		"upload":           "https://storage.example/old.jpg",
		"plot_id":          "deleted-plot",
	})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetTaskAutofill_LocationAndPhotoAreNeverOffered(t *testing.T) {
	db := openLastAnswerTestDB(t)
	autofillSchemaServer(t, farmActivitySchemaJSON)
	user := seedUserAccount(t, db, "farmer_geo_upload", "secret123")
	pastTask := seedTaskFormFor(t, db, "farm_activity", false)
	newTask := seedTaskFormFor(t, db, "farm_activity", false)
	seedResponse(t, db, user.UserID, pastTask, "COMPLETED", time.Now().UTC(), map[string]interface{}{
		"description":  "kept",
		"geo_location": "13.75,100.50",
		"upload":       "https://storage.example/old.jpg",
	})

	w := getAutofill(t, autofillRouter(&FormHandler{DB: db}), newTask, farmerToken(t, user.UserID), "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	assertAutofillAnswer(t, decodeAutofill(t, w).Answer, map[string]interface{}{"description": "kept"})
}

func TestGetTaskAutofill_UnknownTaskIs404(t *testing.T) {
	db := openLastAnswerTestDB(t)
	user := seedUserAccount(t, db, "farmer_unknown_task", "secret123")
	r := autofillRouter(&FormHandler{DB: db})

	if w := getAutofill(t, r, uuid.New(), farmerToken(t, user.UserID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown task: want 404, got %d", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks/not-a-uuid/autofill", nil)
	req.Header.Set("Authorization", "Bearer "+farmerToken(t, user.UserID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("malformed task id: want 404, got %d", w.Code)
	}
}

func assertAutofillAnswer(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	// json.Marshal sorts map keys, so equal maps give byte-identical JSON.
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("answer: want %s, got %s", wantJSON, gotJSON)
	}
}
