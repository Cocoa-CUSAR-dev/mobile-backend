package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// This file covers the LINE login flow (PR #14) that shipped with zero test
// coverage — verifyLineIDToken, VerifyLiffToken, LinkLineAccount. As with
// the rest of this package, anything that needs a real DB (LinkLineAccount's
// success path) isn't covered here; only its pre-DB validation is.

// withLineVerifyServer points lineVerifyURL at a test server for one test;
// t.Cleanup restores the real URL afterward.
func withLineVerifyServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	prev := lineVerifyURL
	lineVerifyURL = server.URL
	t.Cleanup(func() { lineVerifyURL = prev })
}

// --- verifyLineIDToken ------------------------------------------------------

func TestVerifyLineIDToken_MissingChannelID(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "")

	_, _, err := verifyLineIDToken("some-token")
	if err == nil {
		t.Fatal("want error when LINE_CHANNEL_ID is unset, got nil")
	}
}

func TestVerifyLineIDToken_SendsExpectedFormFields(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")

	var gotContentType string
	var gotIDToken, gotClientID string

	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("server failed to parse form: %v", err)
		}
		gotIDToken = r.FormValue("id_token")
		gotClientID = r.FormValue("client_id")
		fmt.Fprint(w, `{"sub":"U1234","name":"Test User"}`)
	})

	sub, name, err := verifyLineIDToken("my-id-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("want form-urlencoded content type, got %q", gotContentType)
	}
	if gotIDToken != "my-id-token" {
		t.Errorf("want id_token=my-id-token, got %q", gotIDToken)
	}
	if gotClientID != "test-channel-id" {
		t.Errorf("want client_id=test-channel-id, got %q", gotClientID)
	}
	if sub != "U1234" || name != "Test User" {
		t.Errorf("want sub=U1234/name=Test User, got sub=%q name=%q", sub, name)
	}
}

func TestVerifyLineIDToken_NonOKStatusIsAnError(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")

	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_request"}`)
	})

	if _, _, err := verifyLineIDToken("expired-or-bad-token"); err == nil {
		t.Fatal("want error on non-200 from LINE, got nil")
	}
}

func TestVerifyLineIDToken_MalformedJSONIsAnError(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")

	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json at all`)
	})

	if _, _, err := verifyLineIDToken("some-token"); err == nil {
		t.Fatal("want error on malformed JSON body, got nil")
	}
}

func TestVerifyLineIDToken_EmptySubIsRejected(t *testing.T) {
	// See PR #14 review: a 200 with no/blank sub used to sail through as a
	// "verified" empty line_user_id. LINE's contract guarantees sub on a
	// real 200, but nothing before this checked that guarantee held.
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")

	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"sub":"","name":"Ghost User"}`)
	})

	if _, _, err := verifyLineIDToken("some-token"); err == nil {
		t.Fatal("want error when LINE's response has an empty sub, got nil")
	}
}

func TestVerifyLineIDToken_TransportErrorIsAnError(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")

	// A closed server guarantees a connection failure without relying on
	// network access or a real unreachable host.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	prev := lineVerifyURL
	lineVerifyURL = server.URL
	server.Close()
	t.Cleanup(func() { lineVerifyURL = prev })

	if _, _, err := verifyLineIDToken("some-token"); err == nil {
		t.Fatal("want error when the LINE endpoint is unreachable, got nil")
	}
}

// --- VerifyLiffToken: pre-network branch -----------------------------------

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

func TestVerifyLiffToken_InvalidJSON(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- VerifyLiffToken: end-to-end against a fake LINE server ----------------

func TestVerifyLiffToken_SuccessPassesThroughLineUserID(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")
	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"sub":"Uabc123","name":"Somchai"}`)
	})

	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/verify", h.VerifyLiffToken)

	req := httptest.NewRequest(http.MethodPost, "/liff/verify", strings.NewReader(`{"idToken":"whatever"}`))
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
	if body["line_user_id"] != "Uabc123" || body["name"] != "Somchai" {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestVerifyLiffToken_LineRejectsTokenReturns401(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "test-channel-id")
	withLineVerifyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

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

// --- LinkLineAccount: pre-DB branches ---------------------------------------
//
// Same limitation as the rest of this package: the success path needs a real
// DB (user lookup, role join, and the INSERT into auth.line_identity), so
// only the validation that happens before any DB/network call is covered
// here.

func TestLinkLineAccount_MissingIDToken(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	req := httptest.NewRequest(http.MethodPost, "/liff/link",
		strings.NewReader(`{"username":"u","password":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestLinkLineAccount_MissingUsername(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	req := httptest.NewRequest(http.MethodPost, "/liff/link",
		strings.NewReader(`{"idToken":"t","password":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestLinkLineAccount_MissingPassword(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	req := httptest.NewRequest(http.MethodPost, "/liff/link",
		strings.NewReader(`{"idToken":"t","username":"u"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestLinkLineAccount_InvalidJSON(t *testing.T) {
	h := &AuthHandler{}
	r := gin.New()
	r.POST("/liff/link", h.LinkLineAccount)

	req := httptest.NewRequest(http.MethodPost, "/liff/link", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}
