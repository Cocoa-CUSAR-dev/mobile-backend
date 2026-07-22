package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FormHandler struct {
	DB *gorm.DB
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

		// Switch case ตาม handler (เหมือนเดิม)
		switch taskForm.Handler {
		case "harvest":
			fmt.Println("ประมวลผล: harvest")
		// ... case อื่นๆ ...
		default:
			fmt.Printf("บันทึกทั่วไปสำหรับ handler: %s\n", taskForm.Handler)
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
