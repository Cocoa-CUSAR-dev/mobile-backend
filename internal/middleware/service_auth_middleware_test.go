package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func newServiceEchoRouter() (*gin.Engine, *bool) {
	ran := false
	r := gin.New()
	r.Use(ServiceAuthMiddleware())
	r.GET("/service/ping", func(c *gin.Context) {
		ran = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r, &ran
}

func TestServiceAuthMiddleware_MissingKeyEnvFailsClosed(t *testing.T) {
	_ = os.Unsetenv("CHATBOT_SERVICE_KEY")
	r, ran := newServiceEchoRouter()

	req := httptest.NewRequest(http.MethodGet, "/service/ping", nil)
	req.Header.Set("X-Service-Key", "anything")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 when CHATBOT_SERVICE_KEY unset, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestServiceAuthMiddleware_MissingHeaderReturns401(t *testing.T) {
	_ = os.Setenv("CHATBOT_SERVICE_KEY", "test-service-key")
	defer func() { _ = os.Unsetenv("CHATBOT_SERVICE_KEY") }()
	r, ran := newServiceEchoRouter()

	req := httptest.NewRequest(http.MethodGet, "/service/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for missing header, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestServiceAuthMiddleware_WrongKeyReturns401(t *testing.T) {
	_ = os.Setenv("CHATBOT_SERVICE_KEY", "test-service-key")
	defer func() { _ = os.Unsetenv("CHATBOT_SERVICE_KEY") }()
	r, ran := newServiceEchoRouter()

	req := httptest.NewRequest(http.MethodGet, "/service/ping", nil)
	req.Header.Set("X-Service-Key", "wrong-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for wrong key, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestServiceAuthMiddleware_CorrectKeyLetsRequestThrough(t *testing.T) {
	_ = os.Setenv("CHATBOT_SERVICE_KEY", "test-service-key")
	defer func() { _ = os.Unsetenv("CHATBOT_SERVICE_KEY") }()
	r, ran := newServiceEchoRouter()

	req := httptest.NewRequest(http.MethodGet, "/service/ping", nil)
	req.Header.Set("X-Service-Key", "test-service-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !*ran {
		t.Error("downstream handler should have run")
	}
}
