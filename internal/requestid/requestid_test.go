package requestid

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, FromContext(c))
	})
	return r
}

func TestMiddleware_GeneratesIDWhenAbsent(t *testing.T) {
	r := newRouter()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	responseHeader := w.Header().Get(Header)
	if responseHeader == "" {
		t.Fatal("want a generated request ID in the response header, got empty")
	}
	if w.Body.String() != responseHeader {
		t.Errorf("want FromContext to return the same ID set in the header (%q), got %q", responseHeader, w.Body.String())
	}
}

func TestMiddleware_EchoesInboundID(t *testing.T) {
	r := newRouter()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(Header, "inbound-id-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get(Header); got != "inbound-id-123" {
		t.Errorf("want inbound ID echoed back, got %q", got)
	}
	if w.Body.String() != "inbound-id-123" {
		t.Errorf("want FromContext to return the inbound ID, got %q", w.Body.String())
	}
}

func TestFromContext_EmptyWhenMiddlewareNotRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := FromContext(c); got != "" {
		t.Errorf("want empty string without Middleware, got %q", got)
	}
}
