package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// ServiceAuthMiddleware gates routes meant for trusted first-party services
// (currently just the chatbot), not farmer sessions. Deliberately separate
// from JwtAuthMiddleware: a farmer's JWT proves "I am this specific user,"
// while this only proves "the caller knows the shared service key" — the
// caller still has to name which user_id it's acting on behalf of, and the
// handler is responsible for checking that claim is legitimate (see
// FormHandler.SubmitTaskForUser's chat.conversation lookup). Mixing the two
// trust models into one middleware would blur that distinction.
func ServiceAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := os.Getenv("CHATBOT_SERVICE_KEY")
		if expected == "" {
			// Fail closed: an unset key must never mean "let everyone in."
			c.JSON(http.StatusInternalServerError, gin.H{"error": "CHATBOT_SERVICE_KEY not configured"})
			c.Abort()
			return
		}

		provided := c.GetHeader("X-Service-Key")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing service key"})
			c.Abort()
			return
		}

		c.Next()
	}
}
