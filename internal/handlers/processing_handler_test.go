package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- RegisterProcessor: pre-DB branches -----------------------------------

func TestRegisterProcessor_MissingUserIDPanics(t *testing.T) {
	// val, _ := c.Get("userID"); userID := val.(uuid.UUID) panics if
	// middleware didn't set userID. We recover and assert 500 so a future
	// hardening (returning 401) is observable.
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/processor", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterProcessor(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/processor",
		strings.NewReader(`{"first_name":"A","last_name":"B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegisterProcessor_InvalidJSON(t *testing.T) {
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/processor", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterProcessor(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/processor",
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

func TestRegisterProcessor_EmptyBody(t *testing.T) {
	// All fields on RegisterProcessorRequest are optional pointers (except
	// BirthDate which is a plain string). Empty body must bind successfully
	// and then reach the DB layer (which nil-panics without a real
	// *gorm.DB). We recover and assert 500 so the bind-vs-DB split is
	// observably confirmed.
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/processor", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterProcessor(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/processor", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- RegisterStation: pre-DB branches -------------------------------------

func TestRegisterStation_MissingUserIDPanics(t *testing.T) {
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/station", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterStation(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/station",
		strings.NewReader(`{"processing_station_name":"S1","hub_id":"h1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

func TestRegisterStation_InvalidJSON(t *testing.T) {
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/station", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterStation(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/station",
		strings.NewReader(`{"processing_station_name":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegisterStation_EmptyBody(t *testing.T) {
	// All fields optional — empty body binds, then panics on nil DB.
	h := &ProcessingHandler{}
	r := gin.New()
	r.POST("/station", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.RegisterStation(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/station", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- GetMyProcessingStation: pre-DB branches ------------------------------

func TestGetMyProcessingStation_MissingUserIDPanics(t *testing.T) {
	h := &ProcessingHandler{}
	r := gin.New()
	r.GET("/stations", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetMyProcessingStation(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/stations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

// --- GetMyBatches: pre-DB branches ----------------------------------------

func TestGetMyBatches_MissingUserIDPanics(t *testing.T) {
	h := &ProcessingHandler{}
	r := gin.New()
	r.GET("/batches", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetMyBatches(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/batches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 from panic-recover, got %d", w.Code)
	}
}

// --- JSON contract tests --------------------------------------------------
//
// These protect against accidental field renames in the request structs
// that would silently break the mobile client.

func TestRegisterProcessorRequest_JSONRoundTrip(t *testing.T) {
	in := `{
		"first_name":"สมชาย",
		"last_name":"ใจดี",
		"nickname":"ชาย",
		"birth_date":"1990-05-20",
		"id_card_number":"1234567890123",
		"address_detail":"123/4",
		"province_id":"50",
		"district_id":"5001",
		"subdistrict_id":"500101",
		"phone_number":"0812345678"
	}`
	var req RegisterProcessorRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.BirthDate != "1990-05-20" {
		t.Errorf("birth_date: want 1990-05-20, got %q", req.BirthDate)
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
	if !strings.Contains(string(out), `"birth_date":"1990-05-20"`) {
		t.Errorf("birth_date missing from marshalled output: %s", out)
	}
}

func TestRegisterProcessorRequest_AllOptional(t *testing.T) {
	// Every field is *string (pointer), so an empty body must bind
	// cleanly without errors. This is the key contract difference vs.
	// agriculture's RegisterFarmerProfileRequest.
	in := `{}`
	var req RegisterProcessorRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.FirstName != nil {
		t.Errorf("first_name should be nil, got %v", *req.FirstName)
	}
	if req.BirthDate != "" {
		t.Errorf("birth_date should be empty, got %q", req.BirthDate)
	}
}

func TestRegisterStationRequest_JSONRoundTrip(t *testing.T) {
	in := `{
		"processing_station_name":"สถานี A",
		"hub_id":"hub-1",
		"address_detail":"123/4",
		"province_id":"50",
		"district_id":"5001",
		"subdistrict_id":"500101",
		"gis":[{"lng":100.5,"lat":13.7}]
	}`
	var req RegisterStationRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.StationName == nil || *req.StationName != "สถานี A" {
		t.Errorf("processing_station_name: want สถานี A, got %v", req.StationName)
	}
	if req.HubID == nil || *req.HubID != "hub-1" {
		t.Errorf("hub_id: want hub-1, got %v", req.HubID)
	}
	if len(req.GIS) != 1 {
		t.Fatalf("GIS: want 1 point, got %d", len(req.GIS))
	}
	pt := req.GIS[0]
	if pt["lng"] != 100.5 || pt["lat"] != 13.7 {
		t.Errorf("GIS coords: want {lng:100.5, lat:13.7}, got %v", pt)
	}

	// Round-trip back to JSON; pointers should serialize.
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"processing_station_name":"สถานี A"`) {
		t.Errorf("processing_station_name missing from marshalled output: %s", out)
	}
}

func TestRegisterStationRequest_GISOptional(t *testing.T) {
	// GIS is []map[string]float64; nil/omitted should bind cleanly.
	in := `{"processing_station_name":"S1","hub_id":"h1"}`
	var req RegisterStationRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.GIS) != 0 {
		t.Errorf("GIS should be empty when omitted, got %v", req.GIS)
	}
}
