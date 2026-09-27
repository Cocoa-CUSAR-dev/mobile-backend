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

func TestFromKeys_ReturnsIDSetByMiddleware(t *testing.T) {
	// gin's log formatter only gets the key map, so this is the path the
	// logger in cmd/main.go actually uses -- FromContext is not reachable
	// from there.
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(contextKey, "inbound-id-123")

	if got := FromKeys(c.Keys); got != "inbound-id-123" {
		t.Errorf("want the ID the middleware stored, got %q", got)
	}
}

func TestFromKeys_FallsBackToDashWhenAbsent(t *testing.T) {
	// Log lines from before the middleware ran (or from a panic recovered
	// outside a request) still need to line up in the output.
	if got := FromKeys(nil); got != "-" {
		t.Errorf("want %q for a missing ID, got %q", "-", got)
	}
	if got := FromKeys(map[any]any{contextKey: ""}); got != "-" {
		t.Errorf("want %q for an empty ID, got %q", "-", got)
	}
}
