package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FormHandler struct {
	DB *gorm.DB
}

// standaloneHandlerTables lists the handlers whose destination table has its
// own generated primary key, so an answer payload can be dissected into an
// INSERT without needing a parent row from outside the submission. The other
// 5 live handlers (farm_activity_fertilizer, farm_activity_chemical,
// harvest_grade_detail, fermentation_batch, drying_batch) are child rows that
// need a parent ID the submission doesn't carry — see task-dissection-design.md.
var standaloneHandlerTables = map[string]string{
	"farm_activity":            "agriculture.farm_activity",
	"processing_record":        "processing.processing_record",
	"farm_pest_disease_record": "agriculture.farm_pest_disease_record",
	"harvest":                  "collection.harvest",
	"batch":                    "processing.batch",
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
			END as status
		FROM form.task t
		LEFT JOIN form.task_form tf ON t.task_id = tf.task_id
		LEFT JOIN form.response r 
			ON t.task_id = r.task_log_id 
			AND r.user_id = ?
		WHERE (? = '' OR DATE(t.open_at) = ?)
		ORDER BY t.open_at DESC
	`

	if err := h.DB.Raw(query, userID, date, date).Scan(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถดึงข้อมูลงานได้"})
		return
	}

	c.JSON(http.StatusOK, tasks)
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

	// แทรก task_id เข้าไปใน answer เพื่อให้เวลา GET กลับมาข้อมูลจะสมบูรณ์
	req.Answer["task_id"] = req.TaskID

	var taskForm struct {
		Handler string
	}
	if err := h.DB.Table("form.task_form").Select("handler").Where("task_id = ?", req.TaskID).First(&taskForm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	if _, supported := standaloneHandlerTables[taskForm.Handler]; !supported {
		c.JSON(http.StatusNotImplemented, gin.H{"error": fmt.Sprintf("handler %q ยังไม่รองรับการบันทึกข้อมูลอัตโนมัติ", taskForm.Handler)})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		response := map[string]interface{}{
			"response_id":  uuid.New(),
			"task_log_id":  req.TaskID, // เก็บไว้อ้างอิงในระบบ DB
			"user_id":      userID,
			"submitted_at": time.Now(),
			"answer":       req.Answer, // ตัวนี้จะมี task_id อยู่ข้างในแล้ว
			"status":       "COMPLETED",
		}

		if err := tx.Table("form.response").Create(&response).Error; err != nil {
			return err
		}

		if err := dissectAnswer(tx, taskForm.Handler, req.Answer); err != nil {
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
