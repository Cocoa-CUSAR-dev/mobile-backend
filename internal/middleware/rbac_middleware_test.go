package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// helper: router with RequireRole mounted directly (no JwtAuthMiddleware in
// front) so each test controls exactly what's in the "roles" context key.
func newRBACRouter(seedRoles []string, seedRolesSet bool, allowed ...string) (*gin.Engine, *bool) {
	ran := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if seedRolesSet {
			c.Set("roles", seedRoles)
		}
		c.Next()
	})
	r.GET("/gated", RequireRole(allowed...), func(c *gin.Context) {
		ran = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r, &ran
}

func TestRequireRole_HasMatchingRoleAllowsThrough(t *testing.T) {
	r, ran := newRBACRouter([]string{"farmer", "hub_collector"}, true, "farmer")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !*ran {
		t.Error("downstream handler should have run")
	}
}

func TestRequireRole_NoMatchingRoleReturns403(t *testing.T) {
	r, ran := newRBACRouter([]string{"processor"}, true, "farmer")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestRequireRole_EmptyRolesReturns403(t *testing.T) {
	// Authenticated (roles key exists) but the slice is empty — e.g. a
	// user who logged in before registering any profile.
	r, ran := newRBACRouter([]string{}, true, "farmer")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestRequireRole_MissingRolesContextReturns403(t *testing.T) {
	// Defensive case: RequireRole mounted without JwtAuthMiddleware ever
	// having set "roles" at all (a wiring mistake). Must fail closed,
	// not panic on the type assertion.
	r, ran := newRBACRouter(nil, false, "farmer")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	if *ran {
		t.Error("downstream handler should not have run")
	}
}

func TestRequireRole_MultipleAllowedRolesORSemantics(t *testing.T) {
	// RequireRole("farmer", "admin") should let either role through.
	r, ran := newRBACRouter([]string{"admin"}, true, "farmer", "admin")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !*ran {
		t.Error("downstream handler should have run")
	}
}

func TestRequireRole_UserWithMultipleRolesNeedsOnlyOneMatch(t *testing.T) {
	r, ran := newRBACRouter([]string{"farmer", "processor"}, true, "processor")
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !*ran {
		t.Error("downstream handler should have run")
	}
}
