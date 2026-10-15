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

// --- Task lists follow form.assignment ---------------------------------------
//
// US4-4 (docs-and-plan#236): a task with assignment rows is shown only to the
// people assigned; a task with none is shown to everyone, as it always was
// (so every pre-existing task keeps working with no backfill). The rule lives
// in queryTasksForUser, which both GET /tasks (the farmer app) and
// GET /service/tasks (the chatbot's LIFF to-do list) go through, so both are
// checked here.
//
// Same RUN_DB_TESTS gate and sibling helpers as the other DB-backed tests in
// this package.

func assignTask(t *testing.T, db *gorm.DB, taskID, userID uuid.UUID) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO form.assignment (task_id, user_id) VALUES (?, ?)", taskID, userID,
	).Error; err != nil {
		t.Fatalf("seed form.assignment: %v", err)
	}
}

func taskIDSet(t *testing.T, w *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var tasks []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	ids := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		id, _ := task["task_id"].(string)
		ids[id] = true
	}
	return ids
}

// tasksSeenByChatbot asks GET /service/tasks as if for this user.
func tasksSeenByChatbot(t *testing.T, db *gorm.DB, userID uuid.UUID) map[string]bool {
	t.Helper()
	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks", h.GetTasksForUser)
	req := httptest.NewRequest(http.MethodGet, "/service/tasks?user_id="+userID.String()+"&page_size=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return taskIDSet(t, w)
}

// tasksSeenByApp asks GET /tasks as the signed-in farmer.
func tasksSeenByApp(t *testing.T, db *gorm.DB, userID uuid.UUID) map[string]bool {
	t.Helper()
	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/tasks", func(c *gin.Context) {
		c.Set("userID", userID)
		h.GetTasks(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/tasks?page_size=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return taskIDSet(t, w)
}

type taskViewer struct {
	name string
	see  func(t *testing.T, db *gorm.DB, userID uuid.UUID) map[string]bool
}

// Every scenario below must hold for both ways of listing tasks.
var taskViewers = []taskViewer{
	{"chatbot GET /service/tasks", tasksSeenByChatbot},
	{"app GET /tasks", tasksSeenByApp},
}

func TestTaskList_TaskWithNoAssignments_IsVisibleToEveryone(t *testing.T) {
	for _, viewer := range taskViewers {
		t.Run(viewer.name, func(t *testing.T) {
			db := openLastAnswerTestDB(t)
			a := seedUserAccount(t, db, "farmer_a", "secret123")
			b := seedUserAccount(t, db, "farmer_b", "secret123")
			legacyTask, _ := seedTaskFormForUserTest(t, db, "farm_activity", time.Now().Add(24*time.Hour), false)

			for _, user := range []uuid.UUID{a.UserID, b.UserID} {
				if !viewer.see(t, db, user)[legacyTask.String()] {
					t.Errorf("a task nobody is assigned to must stay visible to user %s", user)
				}
			}
		})
	}
}

func TestTaskList_AssignedTask_IsVisibleOnlyToItsAssignees(t *testing.T) {
	for _, viewer := range taskViewers {
		t.Run(viewer.name, func(t *testing.T) {
			db := openLastAnswerTestDB(t)
			a := seedUserAccount(t, db, "farmer_a", "secret123")
			b := seedUserAccount(t, db, "farmer_b", "secret123")
			task, _ := seedTaskFormForUserTest(t, db, "farm_activity", time.Now().Add(24*time.Hour), false)
			assignTask(t, db, task, a.UserID)

			if !viewer.see(t, db, a.UserID)[task.String()] {
				t.Errorf("the assigned user must see the task")
			}
			if viewer.see(t, db, b.UserID)[task.String()] {
				t.Errorf("a user who is not assigned must NOT see the task")
			}
		})
	}
}

func TestTaskList_TaskAssignedToSeveralPeople_IsVisibleToEachOfThem(t *testing.T) {
	for _, viewer := range taskViewers {
		t.Run(viewer.name, func(t *testing.T) {
			db := openLastAnswerTestDB(t)
			a := seedUserAccount(t, db, "farmer_a", "secret123")
			b := seedUserAccount(t, db, "farmer_b", "secret123")
			outsider := seedUserAccount(t, db, "farmer_c", "secret123")
			task, _ := seedTaskFormForUserTest(t, db, "farm_activity", time.Now().Add(24*time.Hour), false)
			assignTask(t, db, task, a.UserID)
			assignTask(t, db, task, b.UserID)

			for _, user := range []uuid.UUID{a.UserID, b.UserID} {
				if !viewer.see(t, db, user)[task.String()] {
					t.Errorf("user %s is assigned and must see the task", user)
				}
			}
			if viewer.see(t, db, outsider.UserID)[task.String()] {
				t.Errorf("an outsider must not see the task")
			}
		})
	}
}

// One list, three kinds of task: only the one assigned to somebody else drops out.
func TestTaskList_HidesOnlyTheTasksAssignedToSomeoneElse(t *testing.T) {
	for _, viewer := range taskViewers {
		t.Run(viewer.name, func(t *testing.T) {
			db := openLastAnswerTestDB(t)
			me := seedUserAccount(t, db, "farmer_me", "secret123")
			other := seedUserAccount(t, db, "farmer_other", "secret123")
			future := time.Now().Add(24 * time.Hour)

			legacy, _ := seedTaskFormForUserTest(t, db, "farm_activity", future, false)
			mine, _ := seedTaskFormForUserTest(t, db, "harvest", future, false)
			assignTask(t, db, mine, me.UserID)
			theirs, _ := seedTaskFormForUserTest(t, db, "processing_record", future, false)
			assignTask(t, db, theirs, other.UserID)

			seen := viewer.see(t, db, me.UserID)

			if !seen[legacy.String()] {
				t.Errorf("the unassigned (legacy) task should still be listed")
			}
			if !seen[mine.String()] {
				t.Errorf("the task assigned to me should be listed")
			}
			if seen[theirs.String()] {
				t.Errorf("the task assigned to someone else must not be listed")
			}
			if len(seen) != 2 {
				t.Errorf("want exactly 2 tasks, got %d: %v", len(seen), seen)
			}
		})
	}
}

// The status a person sees comes from their own response, not from being
// assigned: assignment only decides whether the task is listed at all.
func TestTaskList_AssignedTaskKeepsItsOwnStatusLogic(t *testing.T) {
	db := openLastAnswerTestDB(t)
	me := seedUserAccount(t, db, "farmer_me", "secret123")
	now := time.Now().UTC().Truncate(time.Second)
	done, _ := seedTaskFormForUserTest(t, db, "harvest", now.Add(24*time.Hour), false)
	assignTask(t, db, done, me.UserID)
	seedResponse(t, db, me.UserID, done, "COMPLETED", now.Add(-time.Hour), map[string]interface{}{"ok": true})
	todo, _ := seedTaskFormForUserTest(t, db, "farm_activity", now.Add(24*time.Hour), false)
	assignTask(t, db, todo, me.UserID)

	h := &FormHandler{DB: db}
	r := gin.New()
	r.GET("/service/tasks", h.GetTasksForUser)
	req := httptest.NewRequest(http.MethodGet, "/service/tasks?user_id="+me.UserID.String()+"&page_size=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	statuses := taskStatuses(t, w)
	if statuses[done.String()] != "COMPLETED" {
		t.Errorf("assigned + answered: want COMPLETED, got %q", statuses[done.String()])
	}
	if statuses[todo.String()] != "NOT_STARTED" {
		t.Errorf("assigned + unanswered: want NOT_STARTED, got %q", statuses[todo.String()])
	}
}
