// Package services holds business logic that doesn't belong to any one
// HTTP handler -- GO-7 notes there's no separation between HTTP concerns
// and domain logic anywhere in this codebase (everything lives in
// internal/handlers). This package doesn't fix that everywhere in one
// pass; it establishes the seam by pulling out auth's three self-contained
// pieces (JWT issuance, role lookup, LINE token verification), which were
// already called from multiple handler structs (AuthHandler,
// AgricultureHandler, CollectionHandler, ProcessingHandler) rather than
// being tied to one.
package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LineAPIClient calls LINE's own token-verification endpoint. A dedicated,
// timeout-bound client -- http.PostForm (used previously) always runs on
// http.DefaultClient, which has no timeout at all, so a slow/unresponsive
// LINE endpoint would hang this request indefinitely. Exported (capital L)
// so handlers package tests can still redirect it at a local httptest
// server, same as before this moved out of that package.
var LineAPIClient = &http.Client{Timeout: 10 * time.Second}

func GenerateToken(userID uuid.UUID, username string, roles []string) (string, int, error) {
	secretKey := []byte(os.Getenv("JWT_KEY"))
	// JWT_ACCESS_TOKEN_EXPIRATION is in SECONDS (matches the env var name
	// and the convention used in docker-compose.yml / .env.sample).
	// The variable was previously named `expirationMs` and interpreted
	// as milliseconds, which made 3600 mean ~3.6 seconds — almost
	// certainly a bug. Reads now match what the env var name suggests.
	expirationSec, _ := strconv.Atoi(os.Getenv("JWT_ACCESS_TOKEN_EXPIRATION"))
	expirationTime := time.Duration(expirationSec) * time.Second

	// roles is nil for callers that don't have any yet normalize to [] so the "roles"
	// claim always decodes as a JSON array, never `null`.
	if roles == nil {
		roles = []string{}
	}

	claims := jwt.MapClaims{
		"user_id": userID.String(), // ฝัง ID ลงใน Token
		"sub":     username,
		"roles":   roles, // ใช้ตรวจสอบ Permission ใน JwtAuthMiddleware
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(expirationTime).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secretKey)

	return tokenString, expirationSec, err
}

/*ResolveRoles is the single source of truth for a user's roles — the
auth.user_role / auth.role join. Login and GetMe used to each compute
roles a different way (this join vs. checking for rows in the
agriculture.farmer / processing.processor / processing.hub_collector
profile tables), which could disagree: GetMe's old approach could never
report a role like "admin" that only ever exists in auth.user_role, not
in any profile table. Both now call this instead.*/
func ResolveRoles(db *gorm.DB, userID uuid.UUID) []string {
	type roleRow struct {
		RoleName string
	}
	var rows []roleRow
	db.Table("auth.user_role").
		Select("r.role_name").
		Joins("JOIN auth.role r ON r.role_id = auth.user_role.role_id").
		Where("auth.user_role.user_id = ?", userID).
		Scan(&rows)

	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleName)
	}
	return roles
}

// VerifyLineIDToken ส่ง idToken (จาก liff.getIDToken() ฝั่ง JS client) ให้ LINE ตรวจสอบว่า
// ถูกต้อง/ไม่หมดอายุ/ออกให้ channel นี้จริง คืนค่า line_user_id (claim "sub") และชื่อ LINE ที่ verify แล้ว
// ใช้ร่วมกันทั้ง VerifyLiffToken (ทดสอบเฉยๆ) และ LinkLineAccount (ผูกบัญชีจริง)
func VerifyLineIDToken(idToken string) (lineUserID string, name string, err error) {
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

	resp, err := LineAPIClient.Do(req)
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
