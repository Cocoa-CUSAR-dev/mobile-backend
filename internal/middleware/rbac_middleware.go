package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireRole returns a middleware that only lets a request through if the
// authenticated user has at least one of the given roles. It reads the
// "roles" context key set by JwtAuthMiddleware, so it must always be
// mounted after JwtAuthMiddleware in the chain — if "roles" was never set
// (e.g. JwtAuthMiddleware wasn't run first), the request is rejected.
func RequireRole(allowed ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rolesVal, exists := c.Get("roles")
		roles, ok := rolesVal.([]string)
		if !exists || !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "No roles found for this account"})
			c.Abort()
			return
		}

		for _, have := range roles {
			for _, want := range allowed {
				if have == want {
					c.Next()
					return
				}
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to perform this action"})
		c.Abort()
	}
}
