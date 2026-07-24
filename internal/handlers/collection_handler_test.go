package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- _parseDate (helper) ---------------------------------------------------
//
// Lives in agriculture_handler.go but is package-private, so we can test it
// here. Behavior worth locking down: an empty string returns "now-ish" instead
// of the zero time, and a malformed string silently returns the zero time.

func TestParseDate_ValidYMD(t *testing.T) {
	got := _parseDate("2025-03-14")
	want := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestParseDate_EmptyReturnsNow(t *testing.T) {
	before := time.Now()
	got := _parseDate("")
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("empty input should return ~now, got %v (window %v..%v)", got, before, after)
	}
}

func TestParseDate_MalformedReturnsZero(t *testing.T) {
	got := _parseDate("not-a-date")
	if !got.IsZero() {
		t.Errorf("malformed input should return zero time, got %v", got)
	}
}

// --- RegisterHubCollector: pre-DB branches --------------------------------

func TestRegisterHubCollector_MissingUserIDPanics(t *testing.T) {
	// Documents the current behavior: the handler does
	//     val, _ := c.Get("userID")
	//     userID := val.(uuid.UUID)
	// which panics with a type assertion error if userID isn't in the context.
	// Middleware in production always sets it, but a misconfigured route
	// would crash the server. If this test starts failing, the handler has
	// been hardened to return 401 — that's a good thing.
	h := &CollectionHandler{}
	r := gin.New()
	r.POST("/collector", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterHubCollector(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/collector",
		strings.NewReader(`{"first_name":"A"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegisterHubCollector_InvalidJSON(t *testing.T) {
	h := &CollectionHandler{}
	r := gin.New()
	r.POST("/collector", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterHubCollector(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/collector",
		strings.NewReader(`{not json`))
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

func TestRegisterHubCollector_EmptyBodyBindsToZero(t *testing.T) {
	// An empty JSON object should bind successfully. After the bind, the
	// handler touches the DB (h.DB.Begin()) which nil-panics without a
	// real *gorm.DB. The wrapper recovers and returns 500 so the test
	// observably confirms the bind succeeded.
	h := &CollectionHandler{}
	r := gin.New()
	r.POST("/collector", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterHubCollector(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/collector", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- RegisterHub: pre-DB branches -----------------------------------------

func TestRegisterHub_InvalidJSON(t *testing.T) {
	h := &CollectionHandler{}
	r := gin.New()
	r.POST("/hub", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterHub(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/hub", strings.NewReader(`{"hub_name":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegisterHub_MissingUserIDPanics(t *testing.T) {
	// Same pattern as the collector handler.
	h := &CollectionHandler{}
	r := gin.New()
	r.POST("/hub", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterHub(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/hub",
		strings.NewReader(`{"hub_name":"H1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

// --- Request struct sanity checks -----------------------------------------
//
// These are pure-data tests guarding against accidental field renames that
// would silently break the JSON contract with the mobile client.

func TestRegisterHubCollectorRequest_JSONRoundTrip(t *testing.T) {
	in := `{
		"first_name": "สมชาย",
		"last_name": "ใจดี",
		"nickname": "ชาย",
		"birthdate": "1990-05-20",
		"id_card_number": "1234567890123",
		"phone_number": "0812345678",
		"line": "@somchai",
		"facebook": "somchai"
	}`
	var req RegisterHubCollectorRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.BirthDate != "1990-05-20" {
		t.Errorf("birthdate: want 1990-05-20, got %q", req.BirthDate)
	}
	if req.FirstName == nil || *req.FirstName != "สมชาย" {
		t.Errorf("first_name: want สมชาย, got %v", req.FirstName)
	}
	if req.PhoneNumber == nil || *req.PhoneNumber != "0812345678" {
		t.Errorf("phone_number: want 0812345678, got %v", req.PhoneNumber)
	}

	// Round-trip back to JSON; pointers should serialize.
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"birthdate":"1990-05-20"`) {
		t.Errorf("birthdate missing from marshalled output: %s", out)
	}
}

func TestRegisterHubRequest_GISOptional(t *testing.T) {
	// GIS is a []map[string]float64; nil/omitted should bind cleanly.
	in := `{"hub_name":"H1","found_date":"2025-01-01"}`
	var req RegisterHubRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.GIS) != 0 {
		t.Errorf("GIS should be empty when omitted, got %v", req.GIS)
	}
	if req.HubName == nil || *req.HubName != "H1" {
		t.Errorf("hub_name: want H1, got %v", req.HubName)
	}
}

func TestRegisterHubRequest_GISCoordinates(t *testing.T) {
	in := `{"hub_name":"H1","found_date":"2025-01-01","gis":[{"lng":100.5,"lat":13.7}]}`
	var req RegisterHubRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.GIS) != 1 {
		t.Fatalf("want 1 GIS point, got %d", len(req.GIS))
	}
	pt := req.GIS[0]
	if pt["lng"] != 100.5 || pt["lat"] != 13.7 {
		t.Errorf("GIS coords: want {lng:100.5, lat:13.7}, got %v", pt)
	}
}
