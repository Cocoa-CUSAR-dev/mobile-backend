package handlers

import (
	"errors"
	"fmt"
	"go-server-mobile/internal/models"
	"go-server-mobile/internal/services"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	DB *gorm.DB
}

// GO-7: GenerateToken, role resolution, and LINE token verification moved
// to internal/services/auth_service.go — see that file's package comment.
// This wrapper is kept because it's the call shape used throughout this
// handler (h.resolveRoles(userID) reads better than threading h.DB through
// every call site).
//
// Merged with origin/dev: that branch independently improved
// verifyLineIDToken (a lineVerifyURL var for test injection, and a guard
// against LINE returning an empty sub) while this branch was moving it out
// to services/auth_service.go -- both landed there, see that file.
func (h *AuthHandler) resolveRoles(userID uuid.UUID) []string {
	return services.ResolveRoles(h.DB, userID)
}

func jwtCookieName() string {
	if name := os.Getenv("JWT_NAME"); name != "" {
		return name
	}
	return "cocoa_mobile_jwt"
}

// GO-3: no new env var for this -- derived from JWT_NAME so a deployment
// that already customizes the access-token cookie name doesn't need a
// second setting to keep the refresh cookie in step with it.
func refreshCookieName() string {
	return jwtCookieName() + "_refresh"
}

// reissueTokenCookie re-signs a fresh JWT for userID — picking up whatever
// roles they have *right now* — and overwrites the session cookie with it.
// JWTs are stateless: once issued, nothing about a token's claims can
// change until it's replaced. Without this, a role granted by (e.g.)
// RegisterFarmerProfile is invisible to RequireRole until the user logs
// out and back in, because the cookie they're still holding was signed
// before the role existed. Call this right after any DB change that grants
// a role, using the same request's cookie-setting mechanism Login uses.
//
// This does not address the reverse case (a role being revoked while an
// old token is still valid) — there's currently no endpoint anywhere in
// this codebase that revokes a role, so that side of the problem is
// latent, not active. When one is added, it should call this too; until
// then, the token's normal expiry (JWT_ACCESS_TOKEN_EXPIRATION) is the
// only bound on how long a revoked role stays usable.
func reissueTokenCookie(c *gin.Context, db *gorm.DB, userID uuid.UUID) (string, error) {
	var user models.UserAccount
	if err := db.Table("auth.user_account").Where("user_id = ?", userID).First(&user).Error; err != nil {
		return "", err
	}

	roles := services.ResolveRoles(db, userID)
	token, maxAge, err := services.GenerateToken(user.UserID, user.Username, roles)
	if err != nil {
		return "", err
	}

	c.SetCookie(jwtCookieName(), token, maxAge, "/", "", false, true)
	// Also return the token itself -- callers include it in their JSON body
	// (web clients can't rely on the cookie surviving a cross-origin round
	// trip, see resolveTokenString in the middleware) so they need something
	// to store and re-send as Authorization: Bearer.
	return token, nil
}

func (h *AuthHandler) Login(c *gin.Context) {
	// 1. ตรวจสอบว่า Login อยู่แล้วหรือไม่
	jwtName := jwtCookieName()
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
	roles := h.resolveRoles(user.UserID)
	hasProfile := len(roles) > 0

	// 4. สร้าง JWT Token (ส่ง userID และ roles เข้าไปด้วยเพื่อให้ Middleware ตรวจสอบ Permission ได้)
	token, maxAge, err := services.GenerateToken(user.UserID, user.Username, roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	// 5. Set Cookie
	c.SetCookie(jwtName, token, maxAge, "/", "", false, true)

	// GO-3: also issue a refresh token, so this session can be renewed
	// without asking for the password again once the access token expires.
	// Login failing outright over a refresh-token DB write would be a
	// worse experience than just not having a refresh token this session,
	// so this doesn't fail the whole login on error -- only logged.
	if refreshToken, _, err := services.IssueRefreshToken(h.DB, user.UserID); err == nil {
		c.SetCookie(refreshCookieName(), refreshToken, services.RefreshTokenExpirationSeconds(), "/", "", false, true)
	} else {
		log.Printf("issue refresh token for user %s: %v", user.UserID, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "ลงชื่อเข้าใช้สำเร็จ",
		"user_id":     user.UserID,
		"username":    user.Username,
		"roles":       roles, // ส่งกลับเป็น ["admin", "farmer", ...]
		"has_profile": hasProfile,
		// Cross-origin web clients (e.g. the GitHub Pages LIFF build) can't
		// read Set-Cookie at all, so the cookie alone isn't enough to keep
		// them logged in -- also hand back the raw token so they can store
		// it themselves and re-send it as Authorization: Bearer <token>.
		"token": token,
	})
}

// RefreshToken exchanges a still-valid refresh token (from its cookie) for
// a new access token + a rotated refresh token, without asking for the
// password again. Public route (see cmd/main.go) since the whole point is
// to work after the access token has already expired.
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	oldRefreshToken, err := c.Cookie(refreshCookieName())
	if err != nil || oldRefreshToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing refresh token"})
		return
	}

	newRefreshToken, userID, err := services.RotateRefreshToken(h.DB, oldRefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired refresh token"})
		return
	}

	var user models.UserAccount
	if err := h.DB.Table("auth.user_account").Where("user_id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่พบบัญชีผู้ใช้"})
		return
	}

	roles := services.ResolveRoles(h.DB, userID)
	accessToken, maxAge, err := services.GenerateToken(user.UserID, user.Username, roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate token"})
		return
	}

	c.SetCookie(jwtCookieName(), accessToken, maxAge, "/", "", false, true)
	c.SetCookie(refreshCookieName(), newRefreshToken, services.RefreshTokenExpirationSeconds(), "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"message": "ต่ออายุ token สำเร็จ",
		// same reasoning as Login -- web clients need the raw token, not
		// just the cookie, see the comment there.
		"token": accessToken,
	})
}

// VerifyLiffToken เป็น endpoint สำหรับทดสอบเฉยๆ (ใช้กับ static/liff-test/index.html) — verify แล้วคืน line_user_id กลับไปดูตรงๆ
// ไม่ผูกกับบัญชีอะไร ของจริงใช้ LinkLineAccount
func (h *AuthHandler) VerifyLiffToken(c *gin.Context) {
	var req struct {
		IDToken string `json:"idToken"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.IDToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องส่ง idToken มาด้วย"})
		return
	}

	lineUserID, name, err := services.VerifyLineIDToken(req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"line_user_id": lineUserID,
		"name":         name,
	})
}

// LinkLineAccount ผูก LINE identity เข้ากับบัญชีที่มีอยู่แล้ว (เคส "existing farmer" ใน ADR 0002)
// รับทั้ง idToken (จาก liff.getIDToken() ฝั่ง client) และ username/password ของบัญชีเดิม
// verify ทั้งสองอย่างในคำขอเดียว — ใช้กับ static/liff-test/link.html
func (h *AuthHandler) LinkLineAccount(c *gin.Context) {
	var req struct {
		IDToken  string `json:"idToken"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.IDToken == "" || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ต้องส่ง idToken, username, password มาด้วย"})
		return
	}

	// 1. ตรวจสอบบัญชีเดิม (username/password) เหมือน Login()
	var user models.UserAccount
	if err := h.DB.Table("auth.user_account").Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "ชื่อผู้ใช้ หรือ รหัสผ่าน ไม่ถูกต้อง"})
		return
	}
	// PasswordHash is *string (nullable -- LINE-only accounts have none, see
	// DB-3). Same nil-safe check Login() already does; a plain
	// []byte(user.PasswordHash) doesn't compile against a *string and was
	// the actual CI failure here.
	if user.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "ชื่อผู้ใช้ หรือ รหัสผ่าน ไม่ถูกต้อง"})
		return
	}

	// 2. ตรวจสอบ idToken กับ LINE
	lineUserID, lineName, err := services.VerifyLineIDToken(req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// ตรวจ has_profile เหมือน Login() เป๊ะๆ — ใช้ services.ResolveRoles ตัวเดียวกับที่
	// Login/reissueTokenCookie ใช้ (แหล่งความจริงเดียว กัน role ไม่ตรงกันระหว่าง endpoint)
	// เพื่อให้ frontend ตัดสินใจได้ว่าเชื่อมบัญชีเสร็จแล้วต้องพาไปกรอกโปรไฟล์
	// (Role Register Page) ก่อน หรือเสร็จสมบูรณ์แล้วไปหน้า success ได้เลย —
	// ใช้ตรรกะเดียวกับ next_page ของ Login ปกติ ไม่ใช่ตรรกะแยกของ LIFF เอง
	roles := services.ResolveRoles(h.DB, user.UserID)
	hasProfile := len(roles) > 0

	var linkReq models.LineLinkRequest
	linkReq.UserID = user.UserID
	linkReq.LineUserID = lineUserID
	linkReq.DisplayName = lineName
	if err := h.DB.Create(&linkReq).Error; err != nil {
		// log error จริงไว้ (ก่อนหน้านี้ไม่มีเลย ทำให้ debug ไม่ได้ว่าทำไม
		// insert พังจริงๆ — ข้อความที่ตอบกลับ user เป็นแค่สรุปแบบเป็นมิตร)
		fmt.Printf("❌ LinkLineAccount: DB.Create(auth.line_identity) error: %v\n", err)

		// auth.line_identity.line_user_id เป็น UNIQUE — ถ้าชนตรงนี้แปลว่า
		// LINE account นี้ถูกผูกกับบัญชีอื่น (หรือบัญชีนี้เอง) ไปแล้ว ไม่ใช่
		// database error ทั่วไป ต้องแยกข้อความให้ user เข้าใจสถานการณ์จริง
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusConflict, gin.H{"error": "บัญชี LINE นี้ถูกผูกกับบัญชีผู้ใช้ไปแล้ว"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกการผูกบัญชี LINE ได้"})
		return
	}

	// LinkLineAccount ใช้แทน Login สำหรับ flow "ผูกบัญชี LINE" (LIFF) — ต้องออก
	// session token ให้เหมือนกัน ไม่งั้นเกษตรกรที่เข้ามาทางนี้จะไม่มีทาง auth เลย
	// (เดิมฟังก์ชันนี้ไม่ตั้งคุกกี้และไม่คืน token ใด ๆ -- ช่องโหว่จริง). ตั้งคุกกี้ไว้ด้วย
	// เพื่อความสมมาตรกับ Login แต่ตัวที่ client เว็บพึ่งได้จริงคือ "token" ในนี้
	token, maxAge, err := services.GenerateToken(user.UserID, user.Username, roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถสร้าง token ได้"})
		return
	}
	c.SetCookie(jwtCookieName(), token, maxAge, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"user_id":      user.UserID.String(),
		"line_user_id": lineUserID,
		"has_profile":  hasProfile,
		"message":      "verify สำเร็จ และบันทึกการผูกบัญชี LINE เรียบร้อยแล้ว",
		"token":        token,
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

	// แยกดึง Roles ผ่านฟังก์ชันเดียวกับ Login เพื่อไม่ให้ role ไม่ตรงกันระหว่าง endpoint
	roles := h.resolveRoles(userID)

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
