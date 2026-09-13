package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"go-server-mobile/internal/models"
	"go-server-mobile/internal/services"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// --- LinkLineAccount: DB-backed scenarios -----------------------------------
//
// auth_handler_test.go only covers LinkLineAccount's pre-DB validation (bad
// input -> 400). The three scenarios that actually define "new-user vs
// returning-user LINE login" (sprint task "TEST - BE: auth flow tests") all
// live past that point, inside the two DB round-trips:
//
//   1. new user (no matching username)          -> 401
//   2. returning user, not yet linked to LINE    -> 200, row created
//   3. returning user, already linked to LINE    -> 409 (UNIQUE conflict)
//
// Scenario 3 specifically depends on GORM's TranslateError turning a real
// Postgres unique-violation into gorm.ErrDuplicatedKey (see
// internal/database/postgres.go's InitDB, which sets that same option) --
// that's real driver behavior, not something a hand-rolled SQL mock can
// fake reliably. So these run against a real (ephemeral, CI-provisioned)
// Postgres instead. Two adjacent branches (wrong password, invalid LINE
// token) are included too since they're free once the DB is wired up and
// complete the handler's branch coverage, matching how verifyLineIDToken's
// tests cover every branch in auth_handler_test.go.
//
// Gated behind RUN_DB_TESTS so `go test ./...` stays DB-free for anyone
// without a local Postgres -- see the "test" job in .github/workflows/ci.yml
// for how CI provisions one.

// testSchemaDDL is a deliberately minimal mirror of three migrations in the
// separate `database` repo -- V1__baseline.sql (auth.user_account),
// V3__db3_unique_identity_columns.sql (username UNIQUE), and
// V9__line_chat_notify_models.sql (auth.line_identity) -- kept just wide
// enough for LinkLineAccount. It's copied rather than pulled in live because
// `database` is a separate git repo with its own history; if those
// migrations change shape, this needs a manual update to match.
const testSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS auth;

CREATE TABLE IF NOT EXISTS auth.user_account (
	user_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	username character varying NOT NULL UNIQUE,
	password_hash character varying,
	is_password_reset boolean DEFAULT false,
	is_requires_password_reset boolean DEFAULT false,
	created_at timestamp without time zone DEFAULT now() NOT NULL,
	updated_at timestamp without time zone DEFAULT now() NOT NULL
);

CREATE TABLE IF NOT EXISTS auth.line_identity (
	line_identity_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id uuid NOT NULL REFERENCES auth.user_account(user_id),
	line_user_id character varying NOT NULL,
	display_name character varying,
	linked_at timestamp without time zone NOT NULL DEFAULT now(),
	CONSTRAINT uq_line_identity_user UNIQUE (user_id),
	CONSTRAINT uq_line_identity_line_user_id UNIQUE (line_user_id)
);

-- GO-3: mirrors database repo's V19__mobile_refresh_token.sql -- see that
-- migration's comment for why only a hash is stored.
CREATE TABLE IF NOT EXISTS auth.refresh_token (
	refresh_token_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id uuid NOT NULL REFERENCES auth.user_account(user_id),
	token_hash character varying NOT NULL,
	expires_at timestamp without time zone NOT NULL,
	used_at timestamp without time zone,
	revoked_at timestamp without time zone,
	created_at timestamp without time zone NOT NULL DEFAULT now(),
	CONSTRAINT uq_refresh_token_hash UNIQUE (token_hash)
);
`

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// openIntegrationTestDB skips the calling test unless RUN_DB_TESTS is set,
// then returns a connection with a fresh, empty auth.user_account /
// auth.line_identity pair -- same TranslateError setup as production so
// gorm.ErrDuplicatedKey behaves identically to what LinkLineAccount expects.
//
// Deliberately named TEST_DB_* rather than reusing InitDB()'s DB_* names: a
// dev's shell/.env commonly already has the real DB_HOST/DB_USER/DB_PASSWORD
// exported (e.g. pointing at Neon) so the app can run locally. If this
// helper read those same names, opting in with RUN_DB_TESTS=1 would TRUNCATE
// the real database instead of a throwaway one. A disjoint name makes that
// impossible -- you have to opt in twice, deliberately.
func openIntegrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("RUN_DB_TESTS not set -- skipping DB-backed LinkLineAccount tests (see .github/workflows/ci.yml's `test` job for how one is provisioned)")
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		envOrDefault("TEST_DB_HOST", "localhost"),
		envOrDefault("TEST_DB_USER", "postgres"),
		envOrDefault("TEST_DB_PASSWORD", "postgres"),
		envOrDefault("TEST_DB_NAME", "cocoa_test"),
		envOrDefault("TEST_DB_PORT", "5432"),
		envOrDefault("TEST_DB_SSLMODE", "disable"),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "", SingularTable: true},
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("connect to test postgres: %v (start one locally, e.g. `docker run --rm -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16-alpine`)", err)
	}
	if err := db.Exec(testSchemaDDL).Error; err != nil {
		t.Fatalf("apply test schema: %v", err)
	}
	// Isolate each test from the last -- child table first, though CASCADE
	// makes the order irrelevant here.
	if err := db.Exec("TRUNCATE auth.refresh_token, auth.line_identity, auth.user_account CASCADE").Error; err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}

func seedUserAccount(t *testing.T, db *gorm.DB, username, password string) models.UserAccount {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	hashStr := string(hash)
	user := models.UserAccount{Username: username, PasswordHash: &hashStr}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user account: %v", err)
	}
	return user
}

// --- Scenario 1: new user (no account at all) -------------------------------

func TestLinkLineAccount_NewUser_UnknownUsername_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	body := `{"idToken":"whatever","username":"ghost_farmer","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- Adjacent branch: returning user, wrong password ------------------------

func TestLinkLineAccount_ExistingUser_WrongPassword_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	seedUserAccount(t, db, "farmer_wrongpw", "correct-password")

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	body := `{"idToken":"whatever","username":"farmer_wrongpw","password":"wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- Adjacent branch: returning user, correct password, bad LINE token ------

func TestLinkLineAccount_ExistingUser_InvalidLineToken_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	seedUserAccount(t, db, "farmer_badtoken", "correct-password")

	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // LINE says the token is invalid/expired
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	body := `{"idToken":"expired-token","username":"farmer_badtoken","password":"correct-password"}`
	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- Scenario 2: returning user, not yet linked to LINE ---------------------

func TestLinkLineAccount_ExistingUnlinkedUser_LinksSuccessfully(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_new_link", "correct-password")

	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"U_new_line_id","name":"Somchai"}`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	body := `{"idToken":"good-token","username":"farmer_new_link","password":"correct-password"}`
	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		UserID     string `json:"user_id"`
		LineUserID string `json:"line_user_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if resp.UserID != user.UserID.String() {
		t.Errorf("user_id: want %q, got %q", user.UserID.String(), resp.UserID)
	}
	if resp.LineUserID != "U_new_line_id" {
		t.Errorf("line_user_id: want %q, got %q", "U_new_line_id", resp.LineUserID)
	}

	// The point of the handler is the side effect, not just the response --
	// confirm the row actually landed.
	var count int64
	db.Table("auth.line_identity").
		Where("user_id = ? AND line_user_id = ?", user.UserID, "U_new_line_id").
		Count(&count)
	if count != 1 {
		t.Errorf("want exactly 1 line_identity row for this user, got %d", count)
	}
}

// --- Scenario 3: returning user, already linked to LINE ---------------------

func TestLinkLineAccount_ExistingUser_AlreadyLinked_ReturnsConflict(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_already_linked", "correct-password")
	// Simulates a previous, already-successful link request.
	if err := db.Create(&models.LineLinkRequest{
		UserID:      user.UserID,
		LineUserID:  "U_already_linked",
		DisplayName: "Somchai",
	}).Error; err != nil {
		t.Fatalf("seed existing line_identity row: %v", err)
	}

	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A different LINE account trying to attach to the same
		// already-linked user -- still a conflict via uq_line_identity_user.
		_, _ = w.Write([]byte(`{"sub":"U_a_different_line_id","name":"Somchai"}`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	body := `{"idToken":"good-token","username":"farmer_already_linked","password":"correct-password"}`
	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- GO-3: refresh token flow ------------------------------------------------

func cookieValue(t *testing.T, w *httptest.ResponseRecorder, name string) (string, bool) {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value, true
		}
	}
	return "", false
}

func TestLogin_IssuesRefreshTokenCookieAndRow(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_login_refresh", "correct-password")

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/login", h.Login)

	body := `{"username":"farmer_login_refresh","password":"correct-password"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	refreshRaw, ok := cookieValue(t, w, refreshCookieName())
	if !ok || refreshRaw == "" {
		t.Fatal("expected a refresh token cookie to be set on login")
	}

	var count int64
	db.Table("auth.refresh_token").
		Where("user_id = ? AND used_at IS NULL AND revoked_at IS NULL", user.UserID).
		Count(&count)
	if count != 1 {
		t.Errorf("want exactly 1 active refresh_token row for this user, got %d", count)
	}
}

func TestRefreshToken_NoCookie_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/refresh", h.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRefreshToken_ValidToken_RotatesAndReturnsNewCookies(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_refresh_valid", "correct-password")
	rawToken, _, err := services.IssueRefreshToken(db, user.UserID)
	if err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/refresh", h.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshCookieName(), Value: rawToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	if _, ok := cookieValue(t, w, jwtCookieName()); !ok {
		t.Error("expected a new access token cookie")
	}
	newRefresh, ok := cookieValue(t, w, refreshCookieName())
	if !ok || newRefresh == "" {
		t.Fatal("expected a new refresh token cookie")
	}
	if newRefresh == rawToken {
		t.Error("expected the refresh token to rotate, got the same value back")
	}

	// The presented token must now be marked used, not just superseded.
	var usedCount int64
	db.Table("auth.refresh_token").
		Where("user_id = ? AND used_at IS NOT NULL", user.UserID).
		Count(&usedCount)
	if usedCount != 1 {
		t.Errorf("want exactly 1 used refresh_token row, got %d", usedCount)
	}
}

func TestRefreshToken_AlreadyUsedToken_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_refresh_replay", "correct-password")
	rawToken, _, err := services.IssueRefreshToken(db, user.UserID)
	if err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}
	// Redeem it once for real, exactly like a legitimate refresh would.
	if _, _, err := services.RotateRefreshToken(db, rawToken); err != nil {
		t.Fatalf("first rotation should succeed: %v", err)
	}

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/refresh", h.RefreshToken)

	// Replaying the same (now-used) raw token -- e.g. a stolen copy used
	// after the legitimate client already rotated it.
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshCookieName(), Value: rawToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRefreshToken_ExpiredToken_ReturnsUnauthorized(t *testing.T) {
	db := openIntegrationTestDB(t)
	user := seedUserAccount(t, db, "farmer_refresh_expired", "correct-password")
	rawToken, _, err := services.IssueRefreshToken(db, user.UserID)
	if err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}
	if err := db.Table("auth.refresh_token").
		Where("user_id = ?", user.UserID).
		Update("expires_at", "2000-01-01").Error; err != nil {
		t.Fatalf("backdate refresh token: %v", err)
	}

	h := &AuthHandler{DB: db}
	r := gin.New()
	r.POST("/refresh", h.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshCookieName(), Value: rawToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}
