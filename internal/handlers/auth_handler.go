package handlers

import (
	"go-server-mobile/internal/models"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	DB *gorm.DB
}

func GenerateToken(userID uuid.UUID, username string) (string, int, error) {
	secretKey := []byte(os.Getenv("JWT_KEY"))
	expirationMs, _ := strconv.Atoi(os.Getenv("JWT_ACCESS_TOKEN_EXPIRATION"))
	expirationTime := time.Duration(expirationMs) * time.Millisecond

	claims := jwt.MapClaims{
		"user_id": userID.String(), // ฝัง ID ลงใน Token
		"sub":     username,
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(expirationTime).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secretKey)

	return tokenString, int(expirationTime.Seconds()), err
}

func (h *AuthHandler) Login(c *gin.Context) {
	// 1. ตรวจสอบว่า Login อยู่แล้วหรือไม่
	jwtName := os.Getenv("JWT_NAME")
	if jwtName == "" {
		jwtName = "cocoa_mobile_jwt"
	}
	if _, err := c.Cookie(jwtName); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Already logged in"})
		return
	}

	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// 2. ค้นหา User จาก username
	var user models.UserAccount
	if err := h.DB.Table("auth.user_account").Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "ชื่อผู้ใช้ หรือ รหัสผ่าน ไม่ถูกต้อง"})
		return
	}

	// 3. ตรวจสอบ Password
	if user.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "ชื่อผู้ใช้ หรือ รหัสผ่าน ไม่ถูกต้อง"})
		return
	}

	// --- ส่วนการตรวจสอบ Roles และ Profile ---
	var roles []string
	var hasProfile bool

	// 1. ตรวจสอบจาก Table หลัก (auth) เพื่อดูว่าเป็น Admin หรือ Researcher หรือไม่
	// สมมติว่ามี table auth.user_role ที่เก็บ role_id และ user_id
	type RoleResult struct {
		RoleName string
	}
	var dbRoles []RoleResult
	h.DB.Table("auth.user_role").
		Select("r.role_name").
		Joins("JOIN auth.role r ON r.role_id = auth.user_role.role_id").
		Where("auth.user_role.user_id = ?", user.UserID).
		Scan(&dbRoles)

	for _, r := range dbRoles {
		roles = append(roles, r.RoleName)
		hasProfile = true
	}
	// กำจัดค่าซ้ำใน slice (ถ้าจำเป็น) และจัดการ logic hasProfile
	// ---------------------------------------

	// 4. สร้าง JWT Token (ส่ง userID และ roles เข้าไปด้วยเพื่อให้ Middleware ตรวจสอบ Permission ได้)
	username := ""
	if user.Username != nil {
		username = *user.Username
	}
	token, maxAge, err := GenerateToken(user.UserID, username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	// 5. Set Cookie
	c.SetCookie(jwtName, token, maxAge, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"message":     "ลงชื่อเข้าใช้สำเร็จ",
		"user_id":     user.UserID,
		"username":    user.Username,
		"roles":       roles, // ส่งกลับเป็น ["admin", "farmer", ...]
		"has_profile": hasProfile,
	})
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง (รหัสผ่านต้อง 6 ตัวขึ้นไป)"})
		return
	}

	// 1. ตรวจสอบว่ามี Username (เบอร์โทร) นี้ในระบบหรือยัง
	var existingUser models.UserAccount
	if err := h.DB.Where("username = ?", req.Username).First(&existingUser).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "เบอร์โทรศัพท์นี้ถูกใช้งานแล้ว"})
		return
	}

	// 2. Hash Password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกรหัสผ่านได้"})
		return
	}

	// 3. บันทึกลง Database
	passwordHash := string(hashedPassword)
	newUser := models.UserAccount{
		Username:     &req.Username,
		PasswordHash: &passwordHash,
	}

	if err := h.DB.Create(&newUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "เกิดข้อผิดพลาดในการสร้างบัญชี"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "สมัครสมาชิกสำเร็จ",
		"user_id": newUser.UserID,
	})
}
func (h *AuthHandler) GetMe(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var result map[string]interface{}

	// ใช้ Query เดียว Join ทุกตารางที่เกี่ยวข้องเพื่อเอาชื่อสถานที่
	// COALESCE จะเลือกค่าตัวแรกที่ไม่เป็น NULL จากตาราง Profile ต่างๆ
	query := `
        SELECT 
            u.username,
            COALESCE(f.first_name, p.first_name, hc.first_name) as first_name,
            COALESCE(f.last_name, p.last_name, hc.last_name) as last_name,
            COALESCE(f.nickname, p.nickname, hc.nickname) as nickname,
            COALESCE(f.phone_number, p.phone_number, hc.phone_number) as phone_number,
            COALESCE(f.line, p.line, hc.line) as line,
            COALESCE(f.facebook, p.facebook, hc.facebook) as facebook,
            COALESCE(f.zip_code, p.zip_code, hc.zip_code) as zip_code,
            pv.province_name_th as province_name,
            dt.district_name_th as district_name,
            sd.subdistrict_name_th as subdistrict_name
        FROM auth.user_account u
        -- Join กับตาราง Profile ต่างๆ
        LEFT JOIN agriculture.farmer f ON f.user_id = u.user_id
        LEFT JOIN processing.processor p ON p.user_id = u.user_id
        LEFT JOIN processing.hub_collector hc ON hc.user_id = u.user_id
        -- Join กับตารางอ้างอิงสถานที่ (อิงจากตารางใดตารางหนึ่งที่มีค่า)
        LEFT JOIN ref.province_constant pv ON pv.province_id = COALESCE(f.province_id, p.province_id, hc.province_id)
        LEFT JOIN ref.district_constant dt ON dt.district_id = COALESCE(f.district_id, p.district_id, hc.district_id)
        LEFT JOIN ref.subdistrict_constant sd ON sd.subdistrict_id = COALESCE(f.subdistrict_id, p.subdistrict_id, hc.subdistrict_id)
        WHERE u.user_id = ?
    `

	if err := h.DB.Raw(query, userID).Scan(&result).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถดึงข้อมูลโปรไฟล์ได้"})
		return
	}

	// แยกดึง Roles (เนื่องจาก 1 คนอาจมีหลาย Role)
	var roles []string

	// ตรวจสอบ Role จากการมีตัวตนในตาราง Profile เพิ่มเติม
	var counts struct {
		IsFarmer    bool
		IsProcessor bool
		IsCollector bool
	}
	h.DB.Raw(`
        SELECT 
            EXISTS(SELECT 1 FROM agriculture.farmer WHERE user_id = ?) as is_farmer,
            EXISTS(SELECT 1 FROM processing.processor WHERE user_id = ?) as is_processor,
            EXISTS(SELECT 1 FROM processing.hub_collector WHERE user_id = ?) as is_collector
    `, userID, userID, userID).Scan(&counts)

	if counts.IsFarmer {
		roles = append(roles, "farmer")
	}
	if counts.IsProcessor {
		roles = append(roles, "processor")
	}
	if counts.IsCollector {
		roles = append(roles, "hub_collector")
	}

	response := gin.H{
		"user_id": userID,
		"roles":   roles,
	}
	// merge profile เข้าไป
	for k, v := range result {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}
