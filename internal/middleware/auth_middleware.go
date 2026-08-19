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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "เซสชันหมดอายุ กรุณาเข้าสู่ระบบใหม่"})
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

		// stash the raw token too, so handlers can forward it to other services
		c.Set("jwtToken", tokenString)

		c.Next()
	}
}

// OptionalJwtAuthMiddleware parses the JWT cookie and sets userID/jwtToken in
// context exactly like JwtAuthMiddleware when a valid one is present, but
// never rejects the request when it's missing or invalid -- it just proceeds
// without those context values set. For routes that mix genuinely public
// data with per-user data behind the same handler (see RefHandler.
// GetConstants, where e.g. "province" is public but "farm" must resolve
// against the caller's own userID) -- the handler itself checks c.Get
// ("userID") per case and rejects only the cases that actually need it.
func OptionalJwtAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		jwtName := os.Getenv("JWT_NAME")
		secretKey := []byte(os.Getenv("JWT_KEY"))

		tokenString, err := c.Cookie(jwtName)
		if err != nil {
			c.Next()
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return secretKey, nil
		})
		if err != nil || !token.Valid {
			c.Next()
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			userIDStr, _ := claims["user_id"].(string)
			if userID, err := uuid.Parse(userIDStr); err == nil {
				c.Set("userID", userID)
			}
		}
		c.Set("jwtToken", tokenString)

		c.Next()
	}
}