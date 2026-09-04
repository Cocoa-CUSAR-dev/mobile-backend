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
			CASE
				WHEN r.response_id IS NOT NULL THEN 'COMPLETED'
				WHEN NOW() > t.close_at THEN 'OVERDUE'
				ELSE 'NOT_STARTED'
			END AS status
		FROM form.task t
		LEFT JOIN form.task_form tf
			ON t.task_id = tf.task_id
		LEFT JOIN form.response r
			ON t.task_id = r.task_log_id
			AND r.user_id = ?
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
			"response_id":  uuid.New(),
			"task_log_id":  taskID, // เก็บไว้อ้างอิงในระบบ DB
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

	err := h.DB.Table("form.response").
		Select("answer").
		Where("task_log_id = ? AND user_id = ?", taskID, userID).
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

	result := h.DB.Table("form.response").
		Where("task_log_id = ? AND user_id = ?", req.TaskID, userID).
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
