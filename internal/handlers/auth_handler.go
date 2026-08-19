package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-server-mobile/internal/models"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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

// lineAPIClient calls LINE's own token-verification endpoint. A dedicated,
// timeout-bound client -- same reasoning as form_handler.go's
// webBackendClient -- http.PostForm (used previously) always runs on
// http.DefaultClient, which has no timeout at all, so a slow/unresponsive
// LINE endpoint would hang this request indefinitely.
var lineAPIClient = &http.Client{Timeout: 10 * time.Second}

func GenerateToken(userID uuid.UUID, username string) (string, int, error) {
	secretKey := []byte(os.Getenv("JWT_KEY"))
	// JWT_ACCESS_TOKEN_EXPIRATION is in SECONDS (matches the env var name
	// and the convention used in docker-compose.yml / .env.sample).
	// The variable was previously named `expirationMs` and interpreted
	// as milliseconds, which made 3600 mean ~3.6 seconds — almost
	// certainly a bug. Reads now match what the env var name suggests.
	expirationSec, _ := strconv.Atoi(os.Getenv("JWT_ACCESS_TOKEN_EXPIRATION"))
	expirationTime := time.Duration(expirationSec) * time.Second

	claims := jwt.MapClaims{
		"user_id": userID.String(), // ฝัง ID ลงใน Token
		"sub":     username,
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(expirationTime).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secretKey)

	return tokenString, expirationSec, err
}

func (h *AuthHandler) Login(c *gin.Context) {
	// เดิมมีเช็ค "ถ้ามี cookie อยู่แล้วให้บล็อก login ซ้ำ" ตรงนี้ แต่ c.Cookie()
	// เช็คแค่ว่ามี cookie ส่งมาไหม ไม่ได้ verify ว่า token ข้างในยังใช้ได้จริง —
	// cookie ค้างจากรอบก่อน (หมดอายุ, มาจากตอน SameSite/Secure ยังตั้งไม่ถูก,
	// ฯลฯ) เลยบล็อก login ทุกครั้งถัดไปแบบถาวร ทั้งที่ credentials ที่กรอกถูกต้อง
	// — แย่เป็นพิเศษใน LIFF webview เพราะ user ไม่มีทางเคลียร์ cookie เองได้เลย
	// login ด้วย credentials ที่ถูกต้องควรสำเร็จเสมอ แค่ออก cookie ใหม่ทับของเดิม
	jwtName := os.Getenv("JWT_NAME")
	if jwtName == "" {
		jwtName = "cocoa_mobile_jwt"
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
	token, maxAge, err := GenerateToken(user.UserID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	// 5. Set Cookie
	// SameSite=None + Secure=true จำเป็นสำหรับ cross-site request จริงๆ (frontend
	// อยู่ github.io, backend อยู่ onrender.com คนละ origin กัน) — browser จะไม่
	// ยอมส่ง cookie ที่ไม่มี SameSite=None กลับมาให้ endpoint คนละ origin เลย และ
	// ถ้า SameSite=None แต่ Secure=false ก็จะโดน browser ปฏิเสธเก็บ cookie ทิ้ง
	// เงียบๆ เหมือนกัน (ปัญหานี้ซ่อนอยู่มาตลอดเพราะ flow เดิมไม่เคยต้องเรียก
	// protected endpoint ต่อจาก browser context เลย จนกระทั่ง flow ผูกบัญชี LINE
	// ใหม่ที่ต้องเรียก /constants/province ฯลฯ ต่อจาก login ถึงเจอ)
	c.SetSameSite(http.SameSiteNoneMode)
	c.SetCookie(jwtName, token, maxAge, "/", "", true, true)

	c.JSON(http.StatusOK, gin.H{
		"message":     "ลงชื่อเข้าใช้สำเร็จ",
		"user_id":     user.UserID,
		"username":    user.Username,
		"roles":       roles, // ส่งกลับเป็น ["admin", "farmer", ...]
		"has_profile": hasProfile,
	})
}

// verifyLineIDToken ส่ง idToken (จาก liff.getIDToken() ฝั่ง JS client) ให้ LINE ตรวจสอบว่า
// ถูกต้อง/ไม่หมดอายุ/ออกให้ channel นี้จริง คืนค่า line_user_id (claim "sub") และชื่อ LINE ที่ verify แล้ว
// ใช้ร่วมกันทั้ง VerifyLiffToken (ทดสอบเฉยๆ) และ LinkLineIdentity (ผูกบัญชีจริง)
func verifyLineIDToken(idToken string) (lineUserID string, name string, err error) {
	channelID := os.Getenv("LINE_CHANNEL_ID")
	if channelID == "" {
		return "", "", fmt.Errorf("ยังไม่ได้ตั้งค่า LINE_CHANNEL_ID ใน .env")
	}

	form := url.Values{
		"id_token":  {idToken},
		"client_id": {channelID},
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.line.me/oauth2/v2.1/verify", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("สร้างคำขอตรวจสอบ LINE token ไม่สำเร็จ: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := lineAPIClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("เรียก LINE verify endpoint ไม่สำเร็จ: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("อ่านผลลัพธ์จาก LINE ไม่สำเร็จ")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("LINE token ไม่ถูกต้องหรือหมดอายุ")
	}

	var payload struct {
		Sub  string `json:"sub"` // นี่คือ line_user_id
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("อ่าน payload จาก LINE ไม่สำเร็จ")
	}
	return payload.Sub, payload.Name, nil
}

// VerifyLiffToken เป็น endpoint สำหรับทดสอบเฉยๆ (ใช้กับ static/liff-test/index.html) — verify แล้วคืน line_user_id กลับไปดูตรงๆ
// ไม่ผูกกับบัญชีอะไร ของจริงใช้ LinkLineIdentity
func (h *AuthHandler) VerifyLiffToken(c *gin.Context) {
	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.IDToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องส่ง idToken มาด้วย"})
		return
	}

	lineUserID, name, err := verifyLineIDToken(req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"line_user_id": lineUserID,
		"name":         name,
	})
}

// LinkLineIdentity ผูก LINE identity เข้ากับบัญชีที่ login อยู่แล้ว (ตัวตนมาจาก
// JWT cookie ผ่าน middleware.JwtAuthMiddleware ไม่ใช่ username/password ในคำขอ
// อีกต่อไป) — แยกออกจาก login แล้ว (เดิม LinkLineIdentity ทำ login+link พร้อม
// กันในคำขอเดียว ทำให้ user ที่มีบัญชี+เชื่อม LINE ไปแล้วแต่ยังไม่มีโปรไฟล์ ถูก
// บล็อกด้วย error "ผูกไปแล้ว" ทุกครั้งที่ login ซ้ำเข้ามา ทั้งที่แค่จะมากรอก
// โปรไฟล์ต่อ) endpoint นี้ idempotent: ถ้า user ที่ login อยู่เชื่อมกับ LINE
// บัญชีนี้ไปแล้ว ถือว่าสำเร็จเลย ไม่ error — ให้ frontend ไปหน้า "เชื่อมแล้ว"
// แทนหน้า "เชื่อมสำเร็จ" ผ่าน flag already_linked ในคำตอบ
func (h *AuthHandler) LinkLineIdentity(c *gin.Context) {
	val, _ := c.Get("userID")
	userID := val.(uuid.UUID)

	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.IDToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องส่ง idToken มาด้วย"})
		return
	}

	lineUserID, lineName, err := verifyLineIDToken(req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// auth.line_identity.user_id เป็น UNIQUE เช่นกัน (1 user เชื่อมได้แค่ 1
	// บัญชี LINE) — เช็คก่อนว่า user ที่ login อยู่มี line_identity อยู่แล้วหรือยัง
	var existing models.LineIdentity
	err = h.DB.Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		if existing.LineUserID == lineUserID {
			// เชื่อมกับ LINE บัญชีเดียวกันนี้ไปแล้ว — ไม่ใช่ error ถือว่าสำเร็จเลย
			c.JSON(http.StatusOK, gin.H{
				"already_linked": true,
				"line_user_id":   lineUserID,
				"message":        "บัญชีนี้เชื่อมกับ LINE นี้อยู่แล้ว",
			})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "บัญชีนี้ถูกผูกกับ LINE บัญชีอื่นไปแล้ว"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถตรวจสอบสถานะการเชื่อมบัญชีได้"})
		return
	}

	// ยังไม่มี line_identity ของ user นี้ — เช็คต่อว่า LINE user_id นี้ถูกผูก
	// กับบัญชีอื่นไปแล้วหรือยัง (line_user_id ก็ unique เช่นกัน)
	var conflict models.LineIdentity
	if err := h.DB.Where("line_user_id = ?", lineUserID).First(&conflict).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "บัญชี LINE นี้ถูกผูกกับบัญชีผู้ใช้อื่นไปแล้ว"})
		return
	}

	linkReq := models.LineIdentity{
		UserID:      userID,
		LineUserID:  lineUserID,
		DisplayName: &lineName,
	}
	if err := h.DB.Create(&linkReq).Error; err != nil {
		fmt.Printf("❌ LinkLineIdentity: DB.Create(auth.line_identity) error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกการผูกบัญชี LINE ได้"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"already_linked": false,
		"line_user_id":   lineUserID,
		"message":        "เชื่อมบัญชี LINE สำเร็จ",
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
		Username:     req.Username,
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
