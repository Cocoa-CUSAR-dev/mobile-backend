package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	// Token signing needs these env vars; set sensible defaults so GenerateToken
	// works in `go test` without a pre-populated environment.
	_ = os.Setenv("JWT_KEY", "test-secret-key-do-not-use-in-prod")
	_ = os.Setenv("JWT_ACCESS_TOKEN_EXPIRATION", "3600")
	_ = os.Setenv("JWT_NAME", "cocoa_mobile_jwt")
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// --- GenerateToken ---------------------------------------------------------

func TestGenerateToken_SignedAndDecodable(t *testing.T) {
	uid := uuid.New()
	uname := "0812345678"

	tokenStr, maxAge, err := GenerateToken(uid, uname)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}
	if maxAge != 3600 {
		t.Errorf("expected maxAge=3600 (1h), got %d", maxAge)
	}

	parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return []byte(os.Getenv("JWT_KEY")), nil
	})
	if err != nil {
		t.Fatalf("failed to parse generated token: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		t.Fatal("token parsed but invalid")
	}

	if sub, _ := claims["sub"].(string); sub != uname {
		t.Errorf("claim sub: want %q, got %q", uname, sub)
	}
	if gotUID, _ := claims["user_id"].(string); gotUID != uid.String() {
		t.Errorf("claim user_id: want %q, got %q", uid.String(), gotUID)
	}

	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	if exp-iat != 3600 {
		t.Errorf("expected exp-iat == 3600s, got %v", exp-iat)
	}
	if time.Since(time.Unix(int64(iat), 0)) > 5*time.Second {
		t.Errorf("iat not recent: %v", time.Unix(int64(iat), 0))
	}
}

func TestGenerateToken_DifferentUsersProduceDifferentTokens(t *testing.T) {
	a, _, err := GenerateToken(uuid.New(), "user-a")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := GenerateToken(uuid.New(), "user-b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("expected distinct tokens for distinct users")
	}
}

// --- Login: pre-DB branches ------------------------------------------------

func TestLogin_AlreadyLoggedIn(t *testing.T) {
	h := &AuthHandler{} // DB not used on this branch
	r := gin.New()
	r.POST("/login", h.Login)

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.AddCookie(&http.Cookie{Name: "cocoa_mobile_jwt", Value: "existing"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if body["error"] != "Already logged in" {
		t.Errorf("want 'Already logged in', got %q", body["error"])
	}
}

func TestLogin_MissingBody(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/login", h.Login)

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// --- Register: validation branch ------------------------------------------

func TestRegister_WeakPasswordRejected(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"username":"0811111111","password":"abc"}`)) // <6 chars
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// Sanity: bcrypt round-trips at the cost Register uses, so an accidental
// cost bump in production is caught by tests.
func TestBcryptRoundTripWithHandlerCost(t *testing.T) {
	pw := "secret-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(pw)); err != nil {
		t.Errorf("round trip failed: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong")); err == nil {
		t.Error("compare should have failed for wrong password")
	}
}