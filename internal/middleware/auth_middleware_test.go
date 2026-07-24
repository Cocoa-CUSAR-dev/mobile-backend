package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// --- TestMain ------------------------------------------------------------
//
// The middleware reads JWT_NAME and JWT_KEY from the process env at
// request time. We set sane defaults in TestMain so `go test` works
// without a pre-populated environment. The handlers package has its
// own TestMain that sets the same values — keeping both in sync
// means tests in either package pass under the same runner.
func TestMain(m *testing.M) {
	_ = os.Setenv("JWT_NAME", "cocoa_mobile_jwt")
	_ = os.Setenv("JWT_KEY", "test-secret-key-do-not-use-in-prod")
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// helper: spin up a router with the middleware mounted at /. A small
// "echo" downstream handler records whether it ran and what userID
// the middleware injected into the context. This lets us assert both
// "did the request get through" and "did the userID flow correctly".
type echoResult struct {
	ran     bool
	userID  string
	rawBody string
}

func newEchoRouter() (*gin.Engine, *echoResult) {
	res := &echoResult{}
	r := gin.New()
	r.Use(JwtAuthMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		res.ran = true
		if v, ok := c.Get("userID"); ok {
			if uid, ok := v.(uuid.UUID); ok {
				res.userID = uid.String()
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r, res
}

const (
	testCookieName = "cocoa_mobile_jwt"
	testSecret     = "test-secret-key-do-not-use-in-prod"
)

// helper: mint a JWT signed with testSecret and the given claims map.
func mintToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

// helper: mint a JWT with a custom signing method. Used to verify the
// middleware rejects anything that isn't HMAC.
func mintTokenWithMethod(t *testing.T, method jwt.SigningMethod, key interface{}) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, jwt.MapClaims{
		"user_id": uuid.New().String(),
		"sub":     "0812345678",
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	})
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

// --- Missing / empty cookie ----------------------------------------------

func TestJwtAuthMiddleware_NoCookieReturns401(t *testing.T) {
	// No cookie at all — middleware reads c.Cookie(jwtName) and
	// gets an http.ErrNoCookie. The response must 401 with the
	// "Session expired" error string.
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Session expired") {
		t.Errorf("want 'Session expired' in body, got %s", w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_EmptyCookieValueReturns401(t *testing.T) {
	// Cookie present but value is "" — still treated as missing by
	// gin's c.Cookie, so the same 401 path fires.
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: ""})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for empty cookie, got %d (body=%s)", w.Code, w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

// --- Malformed / invalid tokens ------------------------------------------

func TestJwtAuthMiddleware_GarbageTokenReturns401(t *testing.T) {
	// A non-JWT string in the cookie — Parse fails, !token.Valid is
	// true, response is 401 "Invalid token".
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: "not.a.jwt"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid token") {
		t.Errorf("want 'Invalid token' in body, got %s", w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_TamperedSignatureReturns401(t *testing.T) {
	// Sign with one secret, send through middleware that uses a
	// different secret — Parse will fail signature verification.
	tok := mintToken(t, jwt.MapClaims{
		"user_id": uuid.New().String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	_ = os.Setenv("JWT_KEY", "different-secret-than-the-token-was-signed-with")

	// Re-create the router so the middleware reads the new env value.
	// (JwtAuthMiddleware reads JWT_KEY inside the handler closure.)
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 on signature mismatch, got %d (body=%s)", w.Code, w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}

	// Restore the default for subsequent subtests.
	_ = os.Setenv("JWT_KEY", testSecret)
}

func TestJwtAuthMiddleware_ExpiredTokenReturns401(t *testing.T) {
	// exp in the past. jwt.Parse with v5 will return an ErrTokenExpired
	// which the middleware treats as the generic "Invalid token" path.
	tok := mintToken(t, jwt.MapClaims{
		"user_id": uuid.New().String(),
		"exp":     time.Now().Add(-1 * time.Hour).Unix(),
		"iat":     time.Now().Add(-2 * time.Hour).Unix(),
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 on expired token, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid token") {
		t.Errorf("want 'Invalid token' in body, got %s", w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_NoneAlgorithmRejected(t *testing.T) {
	// The classic JWT vulnerability: an attacker sends a token with
	// alg=none and no signature. jwt.Parse with v5 rejects this by
	// default (SigningMethodNone is not allowed unless you explicitly
	// opt in via the keyfunc). The middleware passes `secretKey`
	// directly without inspecting the alg field, but the library still
	// catches it. We pin that behavior here.
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"user_id": uuid.New().String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	// SigningMethodNone.SignedString accepts jwt.UnsafeAllowNoneSignatureType
	// as the key — that's the v5 API.
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign with alg=none: %v", err)
	}

	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: signed})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for alg=none, got %d (body=%s)", w.Code, w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_AnyHMACMethodAccepted(t *testing.T) {
	// Documents current behavior: the middleware's keyfunc returns
	// secretKey without checking the token's signing method. Because
	// HS256, HS384, and HS512 are all HMAC-SHA variants and all
	// accept a []byte key, a token signed with any of them is
	// accepted as long as the signature is valid.
	//
	// The security-relevant check is "reject alg=none" (covered
	// separately in TestJwtAuthMiddleware_NoneAlgorithmRejected) —
	// the library rejects None unless the keyfunc explicitly
	// opts in. The library does NOT reject other-vs-expected HMAC
	// variants; that's a code-level decision the middleware would
	// have to make itself.
	//
	// If you ever harden the keyfunc to enforce HS256 specifically,
	// this test will fail — that's the right signal: the
	// hardening was a deliberate, breaking change.
	cases := []jwt.SigningMethod{
		jwt.SigningMethodHS256,
		jwt.SigningMethodHS384,
		jwt.SigningMethodHS512,
	}
	for _, method := range cases {
		t.Run(method.Alg(), func(t *testing.T) {
			tok := mintTokenWithMethod(t, method, []byte(testSecret))
			r, res := newEchoRouter()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s: want 200, got %d (body=%s)",
					method.Alg(), w.Code, w.Body.String())
			}
			if !res.ran {
				t.Errorf("%s: downstream handler should have run", method.Alg())
			}
		})
	}
}

// --- Claims validation ---------------------------------------------------

func TestJwtAuthMiddleware_MissingUserIDClaimReturns401(t *testing.T) {
	// Token is valid and signed correctly, but the user_id claim is
	// absent. The middleware does:
	//     userIDStr, _ := claims["user_id"].(string)
	//     userID, err := uuid.Parse(userIDStr)
	// so an absent claim yields userIDStr == "" → uuid.Parse("") fails
	// → 401 "Invalid User ID format". This is the current behavior;
	// if it ever changes to allow missing claims, this test fails
	// loudly.
	tok := mintToken(t, jwt.MapClaims{
		"sub": "0812345678",
		"exp": time.Now().Add(time.Hour).Unix(),
		// no user_id
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for missing user_id, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid User ID format") {
		t.Errorf("want 'Invalid User ID format' in body, got %s", w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_MalformedUserIDClaimReturns401(t *testing.T) {
	// user_id present but not a valid UUID.
	tok := mintToken(t, jwt.MapClaims{
		"user_id": "not-a-uuid",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for malformed user_id, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid User ID format") {
		t.Errorf("want 'Invalid User ID format' in body, got %s", w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

func TestJwtAuthMiddleware_UserIDAsNumberRejected(t *testing.T) {
	// If a future code path writes user_id as a number instead of a
	// string, the .(string) assertion in the middleware would yield
	// "" and uuid.Parse would fail. Pin that behavior so a refactor
	// that swaps to JSON number breaks loudly here.
	tok := mintToken(t, jwt.MapClaims{
		"user_id": float64(1234567890), // JSON number
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for numeric user_id, got %d (body=%s)", w.Code, w.Body.String())
	}
	if res.ran {
		t.Error("downstream handler should not have run after abort")
	}
}

// --- Happy path ----------------------------------------------------------

func TestJwtAuthMiddleware_ValidTokenLetsRequestThrough(t *testing.T) {
	// The end-to-end happy path: valid signed token with a valid
	// user_id claim → middleware calls c.Next() → downstream handler
	// runs → c.Get("userID") returns the parsed UUID.
	uid := uuid.New()
	tok := mintToken(t, jwt.MapClaims{
		"user_id": uid.String(),
		"sub":     "0812345678",
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !res.ran {
		t.Error("downstream handler should have run for a valid token")
	}
	if res.userID != uid.String() {
		t.Errorf("userID in context: want %s, got %s", uid.String(), res.userID)
	}
}

func TestJwtAuthMiddleware_ExtraClaimsAreIgnored(t *testing.T) {
	// Forward-compat: if the token carries extra claims (e.g. role,
	// exp_refresh, anything new), the middleware should ignore them
	// and still pass. Pins the current "extract only user_id" contract.
	uid := uuid.New()
	tok := mintToken(t, jwt.MapClaims{
		"user_id":   uid.String(),
		"sub":       "0812345678",
		"role":      "processor",
		"tenant_id": "tenant-abc",
		"custom":    []interface{}{"a", "b"},
		"exp":       time.Now().Add(time.Hour).Unix(),
	})
	r, res := newEchoRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (extra claims should be ignored), got %d (body=%s)",
			w.Code, w.Body.String())
	}
	if res.userID != uid.String() {
		t.Errorf("userID: want %s, got %s", uid.String(), res.userID)
	}
}

// --- Cookie name resolution ----------------------------------------------

func TestJwtAuthMiddleware_CookieNameFromEnv(t *testing.T) {
	// The middleware reads JWT_NAME at request time. Pin that:
	// changing the env var changes which cookie is read.
	uid := uuid.New()
	tok := mintToken(t, jwt.MapClaims{
		"user_id": uid.String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	// Set a different cookie name, leave the standard one empty.
	_ = os.Setenv("JWT_NAME", "alt_session")
	defer func() { _ = os.Setenv("JWT_NAME", testCookieName) }()

	r, res := newEchoRouter()

	// Cookie with the OLD name → 401 (not recognized).
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("old cookie name: want 401, got %d", w.Code)
	}

	// Cookie with the NEW name → 200.
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req2.AddCookie(&http.Cookie{Name: "alt_session", Value: tok})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("new cookie name: want 200, got %d (body=%s)", w2.Code, w2.Body.String())
	}
	if res.userID != uid.String() {
		t.Errorf("userID under alt cookie: want %s, got %s", uid.String(), res.userID)
	}
}

// --- Middleware chaining (c.Next contract) -------------------------------
//
// JwtAuthMiddleware calls c.Next() at the end of the happy path. If it
// forgot to call c.Next() (a real refactor risk), the request would
// pass through but no downstream handler would run. The
// ValidTokenLetsRequestThrough test above covers this implicitly —
// we make it explicit here so a future change to "call c.Next() inside
// an if-branch" is caught.

func TestJwtAuthMiddleware_CallsNextOnSuccess(t *testing.T) {
	// We mount TWO handlers in sequence. The second one records that
	// c.Next() was invoked. If the middleware swallowed the request,
	// the second handler never runs.
	uid := uuid.New()
	tok := mintToken(t, jwt.MapClaims{
		"user_id": uid.String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	second := false
	r := gin.New()
	r.Use(JwtAuthMiddleware())
	r.GET("/protected", func(c *gin.Context) {
		c.Next() // explicitly hand off to the next-in-chain
	}, func(c *gin.Context) {
		second = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !second {
		t.Error("middleware did not call c.Next() — downstream chain skipped")
	}
}
