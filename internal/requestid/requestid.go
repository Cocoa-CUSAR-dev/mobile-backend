// Package requestid propagates a per-request correlation ID (X-2e) across
// service boundaries: mobile-app -> mobile-backend -> web-backend.
package requestid

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Header is the header name used both to accept an inbound ID and to
// forward it when this service calls another one (see
// form_schema_client.go's call to web-backend).
const Header = "X-Request-Id"

const contextKey = "requestID"

// validID is what an inbound X-Request-Id has to look like to be trusted.
// Anything else is replaced with a fresh one rather than rejected -- the
// caller gets a working request, just not the ID it asked for.
//
// Worth validating because the value is echoed back, written to every log
// line for the request, and forwarded verbatim to web-backend. Go's HTTP
// client refuses to write a header containing a control byte, so without
// this an inbound ID with a newline in it would make fetchFormSchema fail
// and take the whole form submission down with a 502 -- a valid answer
// rejected because of a header. web-backend and chatbot enforce the same
// shape, so an ID that survives here survives the next hop too.
var validID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Middleware accepts an inbound X-Request-Id (from mobile-app, or a
// service-to-service caller) or generates one, stores it on the gin
// context for handlers to read and forward downstream, and echoes it back
// in the response header so the caller can correlate its own logs with
// this service's.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(Header)
		if !validID.MatchString(id) {
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

// FromKeys returns the request ID out of a gin context's key map, or "-"
// if there isn't one. Separate from FromContext because gin's log
// formatter is handed a LogFormatterParams, which exposes the key map but
// not the *gin.Context it came from -- see cmd/main.go. gin types
// that map as map[any]any, not map[string]any like gin.Context.Keys.
func FromKeys(keys map[any]any) string {
	if id, ok := keys[contextKey].(string); ok && id != "" {
		return id
	}
	return "-"
}
