package middleware

import (
	"net/http"
	"os"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func JwtAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		jwtName := os.Getenv("JWT_NAME")
		secretKey := []byte(os.Getenv("JWT_KEY"))
		fmt.Println("--- [Debug Auth Middleware] ---")
		fmt.Printf("JWT Name Key: '%s'\n", jwtName)
		fmt.Printf("Secret Key Length: %d\n", len(secretKey))
		tokenString, err := c.Cookie(jwtName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please login again" + jwtName })
			c.Abort()
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return secretKey, nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			// ดึง user_id จาก claims (ตอน login ต้องใส่ไว้ใน token ด้วย)
			userIDStr, _ := claims["user_id"].(string)
			userID, err := uuid.Parse(userIDStr)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid User ID format"})
				c.Abort()
				return
			}
			// เก็บ userID ไว้ใน context เป็น type uuid.UUID
			c.Set("userID", userID)
		}

		c.Next()
	}
}