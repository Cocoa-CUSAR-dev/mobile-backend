// Package logging sets up structured JSON logging for the service (X-2a
// convention, applied here for X-2b): timestamp, level, service, request_id,
// message. request_id on access-log lines comes from requestid.Middleware
// (X-2e), which has to run before GinMiddleware -- see cmd/main.go.
package logging

import (
	"go-server-mobile/internal/requestid"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// Init installs the process-wide JSON logger as the slog default. Call this
// once, before anything else logs.
func Init() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "timestamp"
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler).With("service", "mobile-backend"))
}

// GinMiddleware replaces gin's own plain-text access logger (the
// "[GIN] ... | 200 | ..." lines from gin.Default()) with one line of JSON
// per request via slog, so every stdout line has the same shape.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		slog.Info("http_request",
			"request_id", requestid.FromContext(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}
