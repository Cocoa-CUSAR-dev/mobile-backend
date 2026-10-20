package handlers

import (
	"log/slog"
	"net/http"

	"go-server-mobile/internal/validation"

	"github.com/gin-gonic/gin"
)

type SanitizeAutofillRequest struct {
	Answer    map[string]interface{} `json:"answer"`
	Questions []validation.Question  `json:"questions"`
}

type SanitizeAutofillResponse struct {
	Answer map[string]interface{} `json:"answer"`
}

// SanitizeAutofill is the HTTP boundary for #105 (US2-5) --
// validation.SanitizeAutofillAnswer does the actual work; this just
// unwraps/wraps JSON. A *FormHandler method since US2-13: which fields are
// stale parent ids now comes from form.handler_registry, so it needs the DB
// (through the registry's cache -- one read per 5 minutes, not per call).
func (h *FormHandler) SanitizeAutofill(c *gin.Context) {
	var req SanitizeAutofillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รูปแบบคำขอไม่ถูกต้อง"})
		return
	}
	registry, err := currentHandlerRegistry(h.DB)
	if err != nil {
		// Refuse rather than sanitize without knowing which fields are stale
		// parent ids: offering an old farm_activity_id would attach the new
		// submission to the wrong row. The chatbot treats this like any Go
		// hiccup -- no offer, plain start.
		slog.Error("could not read the handler registry", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "ไม่สามารถอ่านรายการแบบฟอร์มที่รองรับได้"})
		return
	}
	sanitized := validation.SanitizeAutofillAnswer(req.Answer, req.Questions, registry.staleParents)
	c.JSON(http.StatusOK, SanitizeAutofillResponse{Answer: sanitized})
}
