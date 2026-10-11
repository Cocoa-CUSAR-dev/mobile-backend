package handlers

import (
	"net/http"
	"time"

	"go-server-mobile/internal/requestid"
	"go-server-mobile/internal/validation"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// parentPickerHandlers are the 5 child handlers whose submission needs a
// parent row (farm activity / harvest / batch) picked first. The chatbot
// never offers autofill for these (chatbot's src/line/parent_picker.py,
// _PARENT_KIND_BY_HANDLER -- same 5 names), and the app mustn't either: the
// whole point of US2-5 is that the two channels offer the same thing. Reusing
// a stale parent id is also the data-integrity bug the sanitizer's
// staleParentFields rule exists for, so there is very little left worth
// offering on these forms anyway. If the team ever wants autofill here,
// change both channels together.
var parentPickerHandlers = map[string]bool{
	"farm_activity_fertilizer": true,
	"farm_activity_chemical":   true,
	"harvest_grade_detail":     true,
	"fermentation_batch":       true,
	"drying_batch":             true,
}

// TaskAutofillResponse is the 200 body of GET /tasks/:taskId/autofill --
// already sanitized, so the app never sees a value it shouldn't offer.
type TaskAutofillResponse struct {
	SubmittedAt time.Time              `json:"submitted_at"`
	Answer      map[string]interface{} `json:"answer"`
}

// 2d. GET /tasks/:taskId/autofill -- US2-5 (docs-and-plan#98): "use last
// time's answers" for the mobile app, giving the same offer the chatbot
// gives (US2-4).
//
// One call that does the whole thing server-side, rather than the app
// calling last-answer then sanitize like the chatbot does: the app never
// sees unsanitized data, it's one round trip on a slow mobile link, and
// parity holds because both paths end in the very same two functions --
// findLastCompletedAnswer and validation.SanitizeAutofillAnswer.
//
// The user comes from the farmer's JWT only (the protected group's
// JwtAuthMiddleware). There is deliberately no user_id parameter: the
// /service/ version takes one because its caller is a trusted service that
// already resolved the user; a farmer's request must never be able to ask
// for somebody else's last answer.
//
// 204 means "nothing to offer", and is the normal answer for every case the
// chatbot also wouldn't offer in. The app treats any non-200 the same way
// (blank form), so a failure here can never block opening a form.
func (h *FormHandler) GetTaskAutofill(c *gin.Context) {
	val, _ := c.Get("userID")
	userID, ok := val.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please login again"})
		return
	}

	// A malformed id can't name a task, so it's "unknown task" -- answered
	// before it reaches Postgres as an invalid-uuid error.
	taskID, err := uuid.Parse(c.Param("taskId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	var taskForm struct {
		Handler          string
		FormID           uuid.UUID `gorm:"column:form_id"`
		IsMultipleSubmit bool      `gorm:"column:is_multiple_submit"`
	}
	if err := h.DB.Table("form.task_form").
		Select("handler, form_id, COALESCE(is_multiple_submit, FALSE) AS is_multiple_submit").
		Where("task_id = ?", taskID).
		First(&taskForm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบแบบฟอร์มสำหรับงานที่ระบุ"})
		return
	}

	// --- Offer gate: exactly the cases the chatbot doesn't offer in. ---
	if parentPickerHandlers[taskForm.Handler] {
		c.Status(http.StatusNoContent)
		return
	}
	if !taskForm.IsMultipleSubmit {
		// This task already has an answer from this farmer: opening it is
		// EDITING that answer (GetTaskResponse prefills it), not starting a
		// new one, and autofill only ever applies to a new one. A
		// multi-submit form skips this check because opening it again
		// starts a new row, which is a legitimate place to offer -- the app
		// itself then only asks for the first row (later rows have
		// carry-forward instead; mixing two sources would be confusing).
		var alreadyAnswered bool
		if err := h.DB.Raw(
			"SELECT EXISTS (SELECT 1 FROM form.response WHERE task_log_id = ? AND user_id = ?)",
			taskID, userID,
		).Scan(&alreadyAnswered).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถตรวจสอบประวัติการส่งงานได้"})
			return
		}
		if alreadyAnswered {
			c.Status(http.StatusNoContent)
			return
		}
	}

	last, err := h.findLastCompletedAnswer(userID, taskForm.Handler)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถดึงคำตอบครั้งล่าสุดได้"})
		return
	}
	if last == nil {
		c.Status(http.StatusNoContent)
		return
	}

	// The CURRENT form, not the one the last answer was given on -- a
	// choice deleted since then must not be offered (the sanitizer's rule 2).
	schema, err := fetchFormSchema(taskForm.FormID, requestid.FromContext(c))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ไม่สามารถดึงข้อมูลฟอร์มได้: " + err.Error()})
		return
	}

	sanitized := validation.SanitizeAutofillAnswer(last.Answer, activeQuestions(schema))
	if len(sanitized) == 0 {
		// Everything it had was stale, a parent id, a location or a photo.
		// An offer of nothing is worse than no offer.
		c.Status(http.StatusNoContent)
		return
	}

	c.JSON(http.StatusOK, TaskAutofillResponse{SubmittedAt: last.SubmittedAt, Answer: sanitized})
}

// activeQuestions flattens a form's schema into the question list the
// sanitizer takes, skipping hidden sections/questions with the same
// Active() rules ValidateAnswer and the app's own renderer use -- a question
// the farmer can't see shouldn't shape what they're offered.
func activeQuestions(schema validation.FormSchema) []validation.Question {
	var questions []validation.Question
	for _, section := range schema.Sections {
		if !section.Active() {
			continue
		}
		for _, q := range section.Questions {
			if q.Active() && q.FieldName != "" {
				questions = append(questions, q)
			}
		}
	}
	return questions
}
