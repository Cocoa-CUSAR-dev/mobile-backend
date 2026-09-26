// Package requestid propagates a per-request correlation ID (X-2e) across
// service boundaries: mobile-app -> mobile-backend -> web-backend.
package requestid

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Header is the header name used both to accept an inbound ID and to
// forward it when this service calls another one (see
// form_schema_client.go's call to web-backend).
const Header = "X-Request-Id"

const contextKey = "requestID"

// Middleware accepts an inbound X-Request-Id (from mobile-app, or a
// service-to-service caller) or generates one, stores it on the gin
// context for handlers to read and forward downstream, and echoes it back
// in the response header so the caller can correlate its own logs with
// this service's.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(Header)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(contextKey, id)
		c.Header(Header, id)
		c.Next()
	}
}

// FromContext returns the current request's ID, or "" if Middleware wasn't
// run (e.g. a unit test that constructs a bare gin.Context).
func FromContext(c *gin.Context) string {
	return c.GetString(contextKey)
}
