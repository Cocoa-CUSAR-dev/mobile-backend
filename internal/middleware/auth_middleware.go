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

			// ดึง roles จาก claims ให้ handler/middleware ถัดไป (RBAC)
			// jwt.MapClaims decode ผ่าน encoding/json ทำให้ JSON array กลายเป็น []interface{} เสมอ ต้องแปลงเป็น []string เอง 
			// Token เก่าที่ออกก่อนมี claim นี้ (หรือ claim ที่รูปแบบผิด) ถือว่าไม่มี role แทนที่
			// จะ reject ทั้ง token userID ยัง valid อยู่, แค่ไม่มีสิทธิ์ใดๆ
			var roles []string
			if rawRoles, ok := claims["roles"].([]interface{}); ok {
				for _, r := range rawRoles {
					if roleStr, ok := r.(string); ok {
						roles = append(roles, roleStr)
					}
				}
			}
			if roles == nil {
				roles = []string{}
			}
			c.Set("roles", roles)
		}

		// stash the raw token too, so handlers can forward it to other services
		c.Set("jwtToken", tokenString)

		c.Next()
	}
}