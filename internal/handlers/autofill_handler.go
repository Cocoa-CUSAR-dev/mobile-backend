package handlers

import (
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
// unwraps/wraps JSON. A plain function, not a *FormHandler method: it
// touches no DB at all, so there's nothing for it to hold a receiver for.
func SanitizeAutofill(c *gin.Context) {
	var req SanitizeAutofillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รูปแบบคำขอไม่ถูกต้อง"})
		return
	}
	sanitized := validation.SanitizeAutofillAnswer(req.Answer, req.Questions)
	c.JSON(http.StatusOK, SanitizeAutofillResponse{Answer: sanitized})
}
