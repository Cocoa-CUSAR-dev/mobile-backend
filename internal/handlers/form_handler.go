package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go-server-mobile/internal/validation"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FormHandler struct {
	DB *gorm.DB
}

// webBackendClient calls the Kotlin service (web-backend). A single client
// is reused across requests rather than allocating one per call. 30s
// tolerates web-backend's ref-choice schema-resolution cache-miss cost
// (one-time per option field per process lifetime, see BE-5) — once warm,
// real calls land in ~1-2s.
var webBackendClient = &http.Client{Timeout: 30 * time.Second}

// standaloneHandlerTables lists the handlers whose destination table has its
// own generated primary key, so an answer payload can be dissected into an
// INSERT without needing a parent row from outside the submission.
var standaloneHandlerTables = map[string]string{
	"farm_activity":            "agriculture.farm_activity",
	"processing_record":        "processing.processing_record",
	"farm_pest_disease_record": "agriculture.farm_pest_disease_record",
	"harvest":                  "collection.harvest",
	"batch":                    "processing.batch",

	// The 5 previously-blocked child handlers — each needs a parent ID
	// (farm_activity_id / harvest_id / batch_id) that isn't in the
	// submission's own answer fields anywhere else. That's no longer a
	// dissection problem: the chatbot now resolves the parent ID via a
	// farm/station-scoped picker (chatbot's src/line/parent_picker.py)
	// before asking any of the form's real questions, and it arrives here
	// as a normal answer field, filtered through liveColumns like any
	// other. See docs/plans/chatbot-child-handler-design.md.
	"farm_activity_fertilizer": "agriculture.farm_activity_fertilizer",
	"farm_activity_chemical":   "agriculture.farm_activity_chemical",
	"harvest_grade_detail":     "collection.harvest_grade_detail",
	"fermentation_batch":       "processing.fermentation_batch",
	"drying_batch":             "processing.drying_batch",
}

// columnsCache holds table (schema.table) -> set of live column names.
// In-process only; cleared on deploy/restart, which is fine since columns
// only change via a Flyway migration.
var columnsCache sync.Map

// liveColumns returns the real column names for a schema-qualified table,
// used as an allowlist so farmer-controlled answer keys can never target a
// column that doesn't actually exist.
func liveColumns(tx *gorm.DB, table string) (map[string]bool, error) {
	if cached, ok := columnsCache.Load(table); ok {
		return cached.(map[string]bool), nil
	}

	parts := strings.SplitN(table, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("table %q must be schema-qualified", table)
	}

	var names []string
	err := tx.Raw(
		`SELECT column_name FROM information_schema.columns WHERE table_schema = ? AND table_name = ?`,
		parts[0], parts[1],
	).Scan(&names).Error
	if err != nil {
		return nil, err
	}

	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	columnsCache.Store(table, set)
	return set, nil
}

// filterKnownColumns keeps only the answer keys that name a real column on
// the destination table. This is the safety boundary: without it, answer's
// keys would go straight into a dynamic INSERT built from farmer-controlled
// JSON.
func filterKnownColumns(answer map[string]interface{}, columns map[string]bool) map[string]interface{} {
	insertMap := make(map[string]interface{}, len(answer))
	for field, value := range answer {
		if columns[field] {
			insertMap[field] = value
		}
	}
	return insertMap
}

// dissectAnswer inserts the parts of answer that match real destination
// columns for handler into that handler's table, inside the caller's
// transaction. Only valid for handlers in standaloneHandlerTables.
func dissectAnswer(tx *gorm.DB, handler string, answer map[string]interface{}) error {
	table, ok := standaloneHandlerTables[handler]
	if !ok {
		return fmt.Errorf("handler %q not yet supported for dissection", handler)
	}

	columns, err := liveColumns(tx, table)
	if err != nil {
		return err
	}

	insertMap := filterKnownColumns(answer, columns)
	if len(insertMap) == 0 {
		return fmt.Errorf("ไม่มีคำตอบที่ตรงกับคอลัมน์ของ handler %q", handler)
	}

	return tx.Table(table).Create(insertMap).Error
}

// ปรับปรุง Struct เพื่อรับ taskId ใน Payload
type SubmitFormRequest struct {
	TaskID string                 `json:"task_id" binding:"required"`
	Answer map[string]interface{} `json:"answer" binding:"required"`
	// Which response to edit, for PUT /tasks only. Optional so the mobile
	// app keeps working before its own fix lands; when omitted,
	// UpdateTaskResponse edits the caller's MOST RECENT response for the
	// task. Either way exactly one row is touched -- see the predicate in
	// UpdateTaskResponse for why that matters now that one task can hold
	// several responses.
	ResponseID string `json:"response_id"`
}

// 1. GET /tasks — ดูงานทั้งหมด (เหมือนเดิม)
func (h *FormHandler) GetTasks(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	date := c.Query("date")

	var tasks []map[string]interface{}

	// GO-6: this bypasses GORM's query builder (raw SQL + Scan), so the
	// shared Paginate scope doesn't apply -- LIMIT/OFFSET appended directly
	// instead, task_id added as an ORDER BY tiebreaker for stable paging.
	page, size := paginationParams(c)

	query := `
		SELECT
			t.task_id,
			t.title,
			t.description,
			t.open_at,
			t.close_at,
			tf.handler,
			-- A multi-submit form is never "done" just because one response
			-- exists -- the whole point is that a farmer files several rows
			-- against the same task (three grades for one harvest). Marking
			-- it COMPLETED after the first would hide the task and make the
			-- second submission unreachable, which is exactly what the
			-- chatbot picker also had to stop doing. It also matters to the
			-- Flutter app specifically: dynamic_register_page.dart decides
			-- edit-vs-create with status == 'COMPLETED', so reporting
			-- COMPLETED would make the app PUT over the previous row instead
			-- of POSTing a new one. IN_PROGRESS is unknown to the app's
			-- switches, which both fall through to the NOT_STARTED
			-- presentation -- safe, and the create path stays selected.
			--
			-- Phase 1 has no explicit "I'm finished" state to put here (that
			-- needs form.assignment, which is Phase 2), so such a task stays
			-- IN_PROGRESS until close_at passes. Named as a known cost in
			-- the design doc, not an oversight.
			--
			-- The multi-submit arm is written as its own complete ladder so
			-- the single-submit arm below keeps its original precedence
			-- exactly (COMPLETED wins over OVERDUE, as it always has).
			CASE
				WHEN COALESCE(tf.is_multiple_submit, FALSE) THEN
					CASE
						WHEN NOW() > t.close_at THEN 'OVERDUE'
						WHEN has_response.ok THEN 'IN_PROGRESS'
						ELSE 'NOT_STARTED'
					END
				WHEN has_response.ok THEN 'COMPLETED'
				WHEN NOW() > t.close_at THEN 'OVERDUE'
				ELSE 'NOT_STARTED'
			END AS status
		FROM form.task t
		LEFT JOIN form.task_form tf
			ON t.task_id = tf.task_id
		-- EXISTS, not a LEFT JOIN onto form.response: joining multiplied the
		-- task row once per response, which was invisible while a task could
		-- only ever have one but would list the same task three times the
		-- moment a farmer files three grade rows against it. Only "does any
		-- response exist" was ever needed here.
		CROSS JOIN LATERAL (
			SELECT EXISTS (
				SELECT 1 FROM form.response r
				WHERE r.task_log_id = t.task_id
				  AND r.user_id = ?
			) AS ok
		) AS has_response
		WHERE (
			NULLIF(?, '')::date IS NULL
			OR DATE(t.open_at) = NULLIF(?, '')::date
		)
		ORDER BY t.open_at DESC, t.task_id
		LIMIT ? OFFSET ?
	`

	if err := h.DB.Raw(query, userID, date, date, size, page*size).Scan(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถดึงข้อมูลงานได้"})
		return
	}

	c.JSON(http.StatusOK, tasks)
}

// 1b. GET /tasks/:taskId/form — ดึงโครงสร้างฟอร์มสดจาก web-backend (Kotlin)
// โดยส่งต่อ JWT ของ farmer เอง (Bearer) ให้ web-backend ตรวจสอบสิทธิ์
// read:form:assigned เอง — ดู web-backend feat/farmer-scoped-form-auth
func (h *FormHandler) GetTaskForm(c *gin.Context) {
	taskID := c.Param("taskId")

	var taskForm struct {
		FormID  uuid.UUID `gorm:"column:form_id"`
		Version int       `gorm:"column:version"`
	}
	if err := h.DB.Table("form.task_form").
		Select("form_id, version").
		Where("task_id = ?", taskID).
		First(&taskForm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	tokenVal, ok := c.Get("jwtToken")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please login again"})
		return
	}
	token := tokenVal.(string)

	webBackendURL := os.Getenv("WEB_BACKEND_URL")
	if webBackendURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ยังไม่ได้ตั้งค่า WEB_BACKEND_URL"})
		return
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/forms/%s", webBackendURL, taskForm.FormID.String()), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างคำขอไปยังระบบฟอร์มไม่สำเร็จ"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := webBackendClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ไม่สามารถติดต่อระบบฟอร์มได้"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "อ่านข้อมูลฟอร์มจากระบบฟอร์มไม่สำเร็จ"})
		return
	}

	// web-backend wraps responses as { value, error }; unwrap it and attach
	// task_form.version so the mobile app can cache-bust its local copy.
	var kotlinResp struct {
		Value json.RawMessage `json:"value"`
		Error *string         `json:"error"`
	}
	if err := json.Unmarshal(body, &kotlinResp); err != nil {
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(resp.StatusCode, gin.H{"error": kotlinResp.Error})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"form":    kotlinResp.Value,
		"version": taskForm.Version,
	})
}

// 2. POST /tasks — ส่งงาน (ดึง taskId จาก Payload)
func (h *FormHandler) SubmitTask(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req SubmitFormRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุข้อมูลให้ครบถ้วน (รวมถึง task_id)"})
		return
	}

	h.submitAnswerForUser(c, userID, req.TaskID, req.Answer)
}

// SubmitTaskForUserRequest — same shape as SubmitFormRequest plus an
// explicit user_id, since there's no farmer session here to derive it from.
type SubmitTaskForUserRequest struct {
	UserID string                 `json:"user_id" binding:"required"`
	TaskID string                 `json:"task_id" binding:"required"`
	Answer map[string]interface{} `json:"answer" binding:"required"`
}

// 2b. POST /service/tasks — same submission as SubmitTask, but for trusted
// first-party services (currently: the chatbot), gated by
// middleware.ServiceAuthMiddleware instead of a farmer's own JWT cookie.
//
// The caller names user_id explicitly. Before trusting that claim, this
// checks a chat.conversation row actually exists for that user_id + task_id
// — the chatbot only creates one when a real guided-flow conversation
// happened, so this is the boundary that stops a compromised or buggy
// caller from submitting fabricated data for an arbitrary farmer. Without
// it, "the caller knows the service key" alone would be enough to write to
// any farmer's records, which defeats the point of a scoped credential.
func (h *FormHandler) SubmitTaskForUser(c *gin.Context) {
	var req SubmitTaskForUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุข้อมูลให้ครบถ้วน (รวมถึง user_id, task_id)"})
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id ไม่ถูกต้อง"})
		return
	}

	var conversationCount int64
	if err := h.DB.Table("chat.conversation").
		Where("user_id = ? AND task_id = ?", userID, req.TaskID).
		Count(&conversationCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถตรวจสอบสิทธิ์ได้"})
		return
	}
	if conversationCount == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่พบบทสนทนาของผู้ใช้นี้สำหรับงานนี้"})
		return
	}

	h.submitAnswerForUser(c, userID, req.TaskID, req.Answer)
}

// LastAnswerResponse is what GET /service/tasks/last-answer returns -- the
// raw answer JSON from the most recent COMPLETED submission for (user,
// handler), nothing filtered out yet. #105 (US2-5) is where filtering for
// what's actually safe to offer as autofill happens (stale parent IDs,
// OPTION values that no longer resolve in the current form, etc.) -- this
// endpoint is deliberately just the lookup, so both the chatbot and any
// future static-form screen build on the same primitive instead of
// re-deriving it twice.
type LastAnswerResponse struct {
	Handler     string                 `json:"handler"`
	SubmittedAt time.Time              `json:"submitted_at"`
	Answer      map[string]interface{} `json:"answer"`
}

// 2c. GET /service/tasks/last-answer?user_id=...&handler=... -- #100
// (US2-4): "offer reusing my last submission's answers." 404 if this
// (user, handler) pair has no COMPLETED submission yet.
//
// Same trust boundary SubmitTaskForUser already accepts for user_id: the
// caller (currently only the chatbot, gated by ServiceAuthMiddleware)
// names it explicitly, trusted at face value. Unlike SubmitTaskForUser's
// write path there's no chat.conversation row to cross-check against yet
// here -- this is deliberately called BEFORE a farmer has picked/started
// anything, so there's no task-specific row to correlate against. The
// result never leaves the server process: it's only used to build a
// prompt for the same user_id the caller already resolved via its own
// identity check upstream (LINE identity, for the chatbot).
func (h *FormHandler) GetLastAnswer(c *gin.Context) {
	userIDParam := c.Query("user_id")
	handler := c.Query("handler")
	if userIDParam == "" || handler == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุ user_id และ handler"})
		return
	}

	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id ไม่ถูกต้อง"})
		return
	}

	// Raw SQL, not the GORM query builder -- same shape GetTasks already
	// uses for the identical form.task_form join, proven reliable here.
	var result struct {
		SubmittedAt time.Time
		Answer      []byte
	}
	query := `
		SELECT r.submitted_at, r.answer
		FROM form.response r
		JOIN form.task_form tf ON tf.task_id = r.task_log_id
		WHERE r.user_id = ? AND tf.handler = ? AND r.status = 'COMPLETED'
		ORDER BY r.submitted_at DESC
		LIMIT 1
	`
	// GORM's Scan (unlike First) never sets .Error just because zero rows
	// matched -- it silently leaves the struct at its zero value instead.
	// Answer == nil is the actual "nothing found" signal here, not err.
	if err := h.DB.Raw(query, userID, handler).Scan(&result).Error; err != nil || result.Answer == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบประวัติการส่งงานประเภทนี้มาก่อน"})
		return
	}

	var answer map[string]interface{}
	if err := json.Unmarshal(result.Answer, &answer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ข้อมูลคำตอบเดิมเสียหาย"})
		return
	}

	c.JSON(http.StatusOK, LastAnswerResponse{
		Handler:     handler,
		SubmittedAt: result.SubmittedAt,
		Answer:      answer,
	})
}

// validateSubmission is the actual gate #54 wants: fetch formID's schema
// and run answer past it before letting anything write. Can't fetch a
// schema? That's a reject too, same as a bad field — we're not writing
// blind just because Kotlin happened to be down.
func validateSubmission(
	fetchSchema func(uuid.UUID) (validation.FormSchema, error),
	formID uuid.UUID,
	answer map[string]interface{},
) ([]validation.FieldError, error) {
	schema, err := fetchSchema(formID)
	if err != nil {
		return nil, err
	}
	return validation.ValidateAnswer(schema, answer), nil
}

// submitAnswerForUser is the shared dissection path both SubmitTask and
// SubmitTaskForUser use once they've each independently resolved a trusted
// userID (from a farmer's JWT, or — for the service path — from a verified
// chat.conversation match). Callers are responsible for that trust decision;
// this function just does the write.
func (h *FormHandler) submitAnswerForUser(
	c *gin.Context, userID uuid.UUID, taskID string, answer map[string]interface{},
) {
	// แทรก task_id เข้าไปใน answer เพื่อให้เวลา GET กลับมาข้อมูลจะสมบูรณ์
	answer["task_id"] = taskID

	var taskForm struct {
		Handler string
		FormID  uuid.UUID `gorm:"column:form_id"`
	}
	if err := h.DB.Table("form.task_form").Select("handler, form_id").Where("task_id = ?", taskID).First(&taskForm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	if _, supported := standaloneHandlerTables[taskForm.Handler]; !supported {
		c.JSON(http.StatusNotImplemented, gin.H{"error": fmt.Sprintf("handler %q ยังไม่รองรับการบันทึกข้อมูลอัตโนมัติ", taskForm.Handler)})
		return
	}

	// Gate: nothing below this point runs until the answer passes. See
	// validateSubmission above.
	fieldErrs, err := validateSubmission(fetchFormSchema, taskForm.FormID, answer)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ไม่สามารถตรวจสอบข้อมูลฟอร์มได้: " + err.Error()})
		return
	}
	if len(fieldErrs) > 0 {
		details := make([]string, len(fieldErrs))
		for i, fe := range fieldErrs {
			details[i] = fe.Error()
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ผ่านการตรวจสอบ", "details": details})
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		response := map[string]interface{}{
			"response_id": uuid.New(),
			"task_log_id": taskID, // เก็บไว้อ้างอิงในระบบ DB
			// The column, its index, its FK and the Go model field
			// (models.Response.TaskFormID) all already existed -- this insert
			// just never set it, leaving task_form_id NULL on every row in
			// the database. Without it, N responses on one task are
			// indistinguishable by form, which is what multi-submit needs to
			// address them. See the multi-submit design doc's blocker 3.
			"task_form_id": taskForm.FormID,
			"user_id":      userID,
			"submitted_at": time.Now(),
			"answer":       answer, // ตัวนี้จะมี task_id อยู่ข้างในแล้ว
			"status":       "COMPLETED",
		}

		if err := tx.Table("form.response").Create(&response).Error; err != nil {
			return err
		}

		if err := dissectAnswer(tx, taskForm.Handler, answer); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถส่งงานได้: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ส่งงานเรียบร้อยแล้ว"})
}

// 3. GET /tasks/:taskId — ดึงข้อมูล (คืน JSON ตามที่เคยส่งมา)
func (h *FormHandler) GetTaskResponse(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)
	taskID := c.Param("taskId") // สำหรับ GET ยังจำเป็นต้องใช้ URL param เพื่อระบุตัวงาน

	var raw struct {
		Answer []byte
	}

	// Take() had no ORDER BY, so with several responses on one task which
	// row came back was undefined. Deliberately still ONE row and still a
	// bare answer object -- the Flutter app consumes this shape directly --
	// but now it is explicitly the most recent one. Callers that need all
	// of them (and the response_id needed to edit a specific one) use
	// GET /tasks/:taskId/responses below.
	err := h.DB.Table("form.response").
		Select("answer").
		Where("task_log_id = ? AND user_id = ?", taskID, userID).
		Order("submitted_at DESC NULLS LAST").
		Take(&raw).Error

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบประวัติการส่งงานนี้"})
		return
	}

	var answer map[string]interface{}
	if err := json.Unmarshal(raw.Answer, &answer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "parse json failed"})
		return
	}

	c.JSON(http.StatusOK, answer)
}

// TaskResponseItem is one row of GET /tasks/:taskId/responses.
type TaskResponseItem struct {
	ResponseID  uuid.UUID              `json:"response_id"`
	TaskFormID  *uuid.UUID             `json:"task_form_id"`
	SubmittedAt *time.Time             `json:"submitted_at"`
	Status      string                 `json:"status"`
	Answer      map[string]interface{} `json:"answer"`
}

// 3b. GET /tasks/:taskId/responses — every submission this farmer has filed
// against the task, newest first.
//
// Added rather than changing GetTaskResponse's shape: that endpoint returns
// a bare answer object the Flutter app parses directly, and turning it into
// an array would break the app. This is also where a caller gets the
// response_id that PUT /tasks now takes to edit one specific submission --
// without it, "edit the third grade row" isn't expressible.
func (h *FormHandler) GetTaskResponses(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)
	taskID := c.Param("taskId")

	var rows []struct {
		ResponseID  uuid.UUID  `gorm:"column:response_id"`
		TaskFormID  *uuid.UUID `gorm:"column:task_form_id"`
		SubmittedAt *time.Time `gorm:"column:submitted_at"`
		Status      string     `gorm:"column:status"`
		Answer      []byte     `gorm:"column:answer"`
	}

	if err := h.DB.Table("form.response").
		Select("response_id, task_form_id, submitted_at, status, answer").
		Where("task_log_id = ? AND user_id = ?", taskID, userID).
		Order("submitted_at DESC NULLS LAST").
		Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถดึงประวัติการส่งงานได้"})
		return
	}

	// Never null in the JSON -- an empty list is a meaningful answer here
	// ("nothing submitted yet"), and a null would make every caller
	// special-case it.
	items := make([]TaskResponseItem, 0, len(rows))
	for _, row := range rows {
		var answer map[string]interface{}
		if len(row.Answer) > 0 {
			if err := json.Unmarshal(row.Answer, &answer); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "ข้อมูลคำตอบเสียหาย"})
				return
			}
		}
		items = append(items, TaskResponseItem{
			ResponseID:  row.ResponseID,
			TaskFormID:  row.TaskFormID,
			SubmittedAt: row.SubmittedAt,
			Status:      row.Status,
			Answer:      answer,
		})
	}

	c.JSON(http.StatusOK, items)
}

// 4. PUT /tasks — แก้ไขงาน (ดึง taskId จาก Payload)
// Uses the same validateSubmission gate defined above submitAnswerForUser
// (originally landed via #45) — this used to be its own duplicate copy
// before the two branches merged.
func (h *FormHandler) UpdateTaskResponse(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req SubmitFormRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง"})
		return
	}

	// แทรก task_id เข้าไปใน answer ใหม่
	req.Answer["task_id"] = req.TaskID

	// Cheap input validation before the outbound validateSubmission call
	// below -- a malformed response_id shouldn't cost a round-trip to
	// web-backend to find out it was malformed.
	var requestedResponseID *uuid.UUID
	if req.ResponseID != "" {
		parsed, err := uuid.Parse(req.ResponseID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "response_id ไม่ถูกต้อง"})
			return
		}
		requestedResponseID = &parsed
	}

	var taskForm struct {
		FormID uuid.UUID `gorm:"column:form_id"`
	}
	if err := h.DB.Table("form.task_form").Select("form_id").Where("task_id = ?", req.TaskID).First(&taskForm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	// Gate: nothing below this point runs until the answer passes.
	fieldErrs, err := validateSubmission(fetchFormSchema, taskForm.FormID, req.Answer)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ไม่สามารถตรวจสอบข้อมูลฟอร์มได้: " + err.Error()})
		return
	}
	if len(fieldErrs) > 0 {
		details := make([]string, len(fieldErrs))
		for i, fe := range fieldErrs {
			details[i] = fe.Error()
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ผ่านการตรวจสอบ", "details": details})
		return
	}

	// Resolve exactly ONE response_id before updating anything. The old
	// predicate here was `task_log_id = ? AND user_id = ?` with no limit,
	// which updated EVERY response for the task -- harmless while a task
	// could only ever hold one, but silent data corruption the moment
	// multi-submit lets a farmer file three grade rows against one harvest
	// and then edit one of them. See the multi-submit design doc's
	// blocker 2: this had to land before anything could create a second
	// response, not after.
	//
	// user_id stays in every predicate below as the ownership check -- a
	// response_id alone would let any authenticated farmer edit someone
	// else's submission by guessing an id.
	var targetResponseID uuid.UUID
	if requestedResponseID != nil {
		targetResponseID = *requestedResponseID
	} else {
		// Caller didn't say which one (the mobile app, until its own fix
		// lands). Most recent wins -- for a single-response task that IS
		// the only row, so this preserves today's behaviour exactly.
		var latest struct {
			ResponseID uuid.UUID `gorm:"column:response_id"`
		}
		if err := h.DB.Table("form.response").
			Select("response_id").
			Where("task_log_id = ? AND user_id = ?", req.TaskID, userID).
			Order("submitted_at DESC NULLS LAST").
			First(&latest).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบข้อมูลที่ต้องการแก้ไข"})
			return
		}
		targetResponseID = latest.ResponseID
	}

	result := h.DB.Table("form.response").
		Where("response_id = ? AND user_id = ?", targetResponseID, userID).
		Updates(map[string]interface{}{
			"answer":       req.Answer,
			"submitted_at": time.Now(),
		})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถแก้ไขข้อมูลได้"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบข้อมูลที่ต้องการแก้ไข"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "แก้ไขข้อมูลเรียบร้อยแล้ว"})
}
