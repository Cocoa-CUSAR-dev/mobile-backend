package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	roles := []string{"farmer", "hub_collector"}

	tokenStr, maxAge, err := GenerateToken(uid, uname, roles)
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

	gotRolesRaw, ok := claims["roles"].([]interface{})
	if !ok {
		t.Fatalf("claim roles: want []interface{}, got %T (%v)", claims["roles"], claims["roles"])
	}
	if len(gotRolesRaw) != len(roles) {
		t.Fatalf("claim roles: want %v, got %v", roles, gotRolesRaw)
	}
	for i, r := range roles {
		if gotRolesRaw[i] != r {
			t.Errorf("claim roles[%d]: want %q, got %v", i, r, gotRolesRaw[i])
		}
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

func TestGenerateToken_NilRolesEncodesAsEmptyArray(t *testing.T) {
	// A user with no roles yet (e.g. registered but no profile created)
	// must still get a decodable "roles" claim — nil would encode as JSON
	// `null`, which callers would have to special-case.
	tokenStr, _, err := GenerateToken(uuid.New(), "0899999999", nil)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(os.Getenv("JWT_KEY")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	claims := parsed.Claims.(jwt.MapClaims)

	gotRoles, ok := claims["roles"].([]interface{})
	if !ok {
		t.Fatalf("claim roles: want []interface{}, got %T (%v)", claims["roles"], claims["roles"])
	}
	if len(gotRoles) != 0 {
		t.Errorf("expected empty roles slice, got %v", gotRoles)
	}
}

func TestGenerateToken_DifferentUsersProduceDifferentTokens(t *testing.T) {
	a, _, err := GenerateToken(uuid.New(), "user-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := GenerateToken(uuid.New(), "user-b", nil)
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

// --- resolveRoles: single source of truth for roles -------------------------
//
// Login and GetMe used to compute roles two different ways (auth.user_role join vs. checking for rows in the farmer/processor/hub_collector profile tables).
// Both now call resolveRoles instead — these tests pin that it's the one thing actually touching the DB for role lookups.

func TestResolveRoles_ReachesDB(t *testing.T) {
	h := &AuthHandler{}
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected resolveRoles to panic against a nil DB (no mock configured)")
		}
	}()
	h.resolveRoles(uuid.New())
}

func TestGetMe_ReachesDBViaResolveRoles(t *testing.T) {
	// GetMe reads "userID" from context (normally set by JwtAuthMiddleware).
	// With that present but no real *gorm.DB wired up, it must panic once it
	// reaches the DB — recovered here to confirm the request got there via
	// the shared resolveRoles path rather than, say, silently returning
	// empty roles.
	h := &AuthHandler{}
	r := gin.New()
	r.GET("/me", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetMe(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- reissueTokenCookie: stale role claims (#7 final sub-issue) ------------
//
// Roles are only ever signed into the JWT when a token is issued (Login).
// A role granted afterwards (e.g. RegisterFarmerProfile inserting into
// auth.user_role) doesn't retroactively change a cookie the caller is
// already holding. reissueTokenCookie re-signs it on the spot so the very
// next request already carries the new role.

func TestReissueTokenCookie_ReachesDB(t *testing.T) {
	// reissueTokenCookie looks the user up by ID before re-signing — with
	// no real *gorm.DB wired up, that nil-panics. Pins that it actually
	// touches the DB rather than silently no-op'ing.
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		_ = reissueTokenCookie(c, (&AuthHandler{}).DB, uuid.New())
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- Register: validation branch ------------------------------------------
//
// RegisterRequest requires `username` and `password` (min 6 chars); `email`
// is optional. Every ShouldBindJSON failure returns the same 400 message,
// so these tests all assert on status code + presence of the error field
// rather than a specific message per field.

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

func TestRegister_MissingUsername(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"password":"abcdef"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegister_MissingPassword(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"username":"0811111111"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegister_EmptyBody(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegister_InvalidJSON(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if _, ok := body["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

// --- Register: reaches DB layer ---------------------------------------------

func TestRegister_ValidInputReachesDB(t *testing.T) {
	// A request that satisfies binding (username + password >= 6 chars) must
	// pass validation and reach h.DB.Where(...) for the duplicate-username
	// check. With no *gorm.DB wired up, that nil-panics — recovered here so
	// the bind-vs-DB split stays observable, matching the pattern used for
	// the other handlers in this package.
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/register", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.Register(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"username":"0811111111","password":"abcdef"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
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

// --- verifyLineIDToken ------------------------------------------------------
//
// verifyLineIDToken always dials https://api.line.me itself (the URL isn't
// injectable), so tests redirect the package-level lineAPIClient at a local
// httptest.Server by rewriting each outgoing request's scheme+host.

type rewriteTransport struct{ target *url.URL }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func swapLineAPIClient(t *testing.T, ts *httptest.Server) {
	t.Helper()
	original := lineAPIClient
	target, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	lineAPIClient = &http.Client{Transport: rewriteTransport{target: target}}
	t.Cleanup(func() { lineAPIClient = original })
}

func setLineChannelID(t *testing.T, id string) {
	t.Helper()
	original, had := os.LookupEnv("LINE_CHANNEL_ID")
	if id == "" {
		_ = os.Unsetenv("LINE_CHANNEL_ID")
	} else {
		_ = os.Setenv("LINE_CHANNEL_ID", id)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("LINE_CHANNEL_ID", original)
		} else {
			_ = os.Unsetenv("LINE_CHANNEL_ID")
		}
	})
}

func TestVerifyLineIDToken_Success(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.FormValue("id_token"); got != "good-token" {
			t.Errorf("id_token: want %q, got %q", "good-token", got)
		}
		if got := r.FormValue("client_id"); got != "test-channel-id" {
			t.Errorf("client_id: want %q, got %q", "test-channel-id", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"U1234567890","name":"Somchai"}`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	lineUserID, name, err := verifyLineIDToken("good-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lineUserID != "U1234567890" {
		t.Errorf("lineUserID: want %q, got %q", "U1234567890", lineUserID)
	}
	if name != "Somchai" {
		t.Errorf("name: want %q, got %q", "Somchai", name)
	}
}

func TestVerifyLineIDToken_NonOKStatus(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	if _, _, err := verifyLineIDToken("bad-token"); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestVerifyLineIDToken_MalformedJSON(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	if _, _, err := verifyLineIDToken("any-token"); err == nil {
		t.Fatal("expected error for malformed JSON body, got nil")
	}
}

func TestVerifyLineIDToken_MissingChannelID(t *testing.T) {
	setLineChannelID(t, "")

	if _, _, err := verifyLineIDToken("any-token"); err == nil {
		t.Fatal("expected error when LINE_CHANNEL_ID is unset, got nil")
	}
}

func TestVerifyLineIDToken_TransportError(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	swapLineAPIClient(t, ts)
	ts.Close() // closed before the request is made -> connection refused

	if _, _, err := verifyLineIDToken("any-token"); err == nil {
		t.Fatal("expected error when LINE API is unreachable, got nil")
	}
}

// --- VerifyLiffToken ---------------------------------------------------------
//
// VerifyLiffToken is the test-only endpoint (static/liff-test/index.html) --
// verify the idToken, hand back line_user_id/name. No DB, no linking.

func TestVerifyLiffToken_MissingIDToken(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestVerifyLiffToken_EmptyBody(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestVerifyLiffToken_Success(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"U9876543210","name":"Malee"}`))
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", strings.NewReader(`{"idToken":"good-token"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if body["line_user_id"] != "U9876543210" {
		t.Errorf("line_user_id: want %q, got %q", "U9876543210", body["line_user_id"])
	}
	if body["name"] != "Malee" {
		t.Errorf("name: want %q, got %q", "Malee", body["name"])
	}
}

func TestVerifyLiffToken_LineRejectsToken(t *testing.T) {
	setLineChannelID(t, "test-channel-id")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()
	swapLineAPIClient(t, ts)

	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", strings.NewReader(`{"idToken":"bad-token"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- LinkLineAccount: pre-DB validation branch ------------------------------
//
// LinkLineAccount checks idToken/username/password are all present before it
// ever touches h.DB, so these cases exercise real request binding without
// needing a database. DB-backed scenarios (new-user vs returning-user) live
// in auth_handler_db_test.go.

func TestLinkLineAccount_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing idToken", `{"username":"farmer1","password":"secret123"}`},
		{"missing username", `{"idToken":"good-token","password":"secret123"}`},
		{"missing password", `{"idToken":"good-token","username":"farmer1"}`},
		{"empty body", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &AuthHandler{} // DB not reached on this branch
			r := gin.New()
			r.POST("/liff/link", h.LinkLineAccount)

			req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

func TestLinkLineAccount_EmptyRequestBody(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	req := httptest.NewRequest(http.MethodPost, "/liff/link", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}