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

// --- buildWKT (GIS → PostGIS WKT) -----------------------------------------
//
// The closing-vertex rule for POLYGONs is easy to break in a refactor:
// WKT requires the last coordinate to equal the first. A test that fails
// when the closing vertex is dropped is the only way to catch that.

func TestBuildWKT_EmptyReturnsEmpty(t *testing.T) {
	if got := buildWKT(nil); got != "" {
		t.Errorf("nil input: want empty string, got %q", got)
	}
	if got := buildWKT([]map[string]float64{}); got != "" {
		t.Errorf("empty input: want empty string, got %q", got)
	}
}

func TestBuildWKT_OnePointReturnsEmpty(t *testing.T) {
	// A single point is handled separately as POINT() in RegisterFarm;
	// buildWKT is only responsible for polygons.
	pts := []map[string]float64{{"lng": 100.5, "lat": 13.7}}
	if got := buildWKT(pts); got != "" {
		t.Errorf("one point: want empty (caller uses POINT()), got %q", got)
	}
}

func TestBuildWKT_TwoPointsClosedRing(t *testing.T) {
	// Note: WKT POLYGON with only 2 distinct points is degenerate but still
	// valid WKT. We just check the ring is closed (last == first).
	pts := []map[string]float64{
		{"lng": 100.0, "lat": 13.0},
		{"lng": 100.1, "lat": 13.1},
	}
	got := buildWKT(pts)
	want := "POLYGON((100.000000 13.000000,100.100000 13.100000,100.000000 13.000000))"
	if got != want {
		t.Errorf("two-point ring not closed correctly:\n want %q\n  got %q", want, got)
	}
}

func TestBuildWKT_FourPointSquare(t *testing.T) {
	pts := []map[string]float64{
		{"lng": 100.0, "lat": 13.0},
		{"lng": 100.1, "lat": 13.0},
		{"lng": 100.1, "lat": 13.1},
		{"lng": 100.0, "lat": 13.1},
	}
	got := buildWKT(pts)
	want := "POLYGON((" +
		"100.000000 13.000000," +
		"100.100000 13.000000," +
		"100.100000 13.100000," +
		"100.000000 13.100000," +
		"100.000000 13.000000))"
	if got != want {
		t.Errorf("square ring:\n want %q\n  got %q", want, got)
	}
	// Belt-and-braces: ensure the first and last coordinate in the
	// ring literal are equal — that's the WKT spec.
	if !strings.HasPrefix(got, "POLYGON((100.000000 13.000000,") {
		t.Error("first vertex mismatch")
	}
	if !strings.HasSuffix(got, "100.000000 13.000000))") {
		t.Error("last vertex does not close the ring")
	}
}

func TestBuildWKT_MissingKeys(t *testing.T) {
	// No "lng" or "lat" keys → the formatter prints "0.000000 0.000000".
	// The build should not panic; the resulting WKT is degenerate but
	// the helper is expected to be called only after a len>1 check, so
	// caller is responsible for catching zero coords.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("buildWKT panicked on missing keys: %v", r)
		}
	}()
	pts := []map[string]float64{{}, {}}
	got := buildWKT(pts)
	if got == "" {
		t.Error("expected non-empty WKT for 2 zero-coord points")
	}
}

// --- RegisterFarmerProfile: pre-DB branches -------------------------------

func TestRegisterFarmerProfile_MissingUserIDReturns401(t *testing.T) {
	// Unlike RegisterFarm/RegisterPlot, this handler checks `exists`
	// before the type assertion, so it should return 401, not panic.
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/farmer", h.RegisterFarmerProfile)

	req := httptest.NewRequest(http.MethodPost, "/farmer",
		strings.NewReader(`{"first_name":"A","last_name":"B","id_card_number":"x","phone_number":"y","province_id":"p","district_id":"d","subdistrict_id":"s"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Unauthorized") {
		t.Errorf("expected 'Unauthorized' in body, got %s", w.Body.String())
	}
}

func TestRegisterFarmerProfile_InvalidJSON(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/farmer", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterFarmerProfile(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/farmer",
		strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ข้อมูลไม่ถูกต้อง") {
		t.Errorf("expected Thai error prefix in body, got %s", w.Body.String())
	}
}

func TestRegisterFarmerProfile_MissingRequiredFields(t *testing.T) {
	// All required fields empty — gin binding should reject.
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/farmer", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterFarmerProfile(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/farmer", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty body, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestRegisterFarm_InvalidJSON(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/farms", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterFarm(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/farms",
		strings.NewReader(`{"farm_name":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestRegisterPlot_InvalidJSON(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/plots", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterPlot(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/plots",
		strings.NewReader(`{"plot_name":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestRegisterPlot_MissingRequiredFields(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.POST("/plots", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.RegisterPlot(c)
	})

	// Empty body — gin binding rejects missing required fields.
	req := httptest.NewRequest(http.MethodPost, "/plots", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// --- GetMyFarms / GetMyPlots: 401 path ------------------------------------

func TestGetMyFarms_MissingUserIDReturns401(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.GET("/farms", h.GetMyFarms)

	req := httptest.NewRequest(http.MethodGet, "/farms", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestGetMyPlots_MissingUserIDReturns401(t *testing.T) {
	h := &AgricultureHandler{}
	r := gin.New()
	r.GET("/plots", h.GetMyPlots)

	req := httptest.NewRequest(http.MethodGet, "/plots", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// --- JSON contract tests --------------------------------------------------
//
// These protect against accidental field renames in the request structs,
// which would silently break the mobile client without a compile error.

func TestRegisterFarmerProfileRequest_FullPayload(t *testing.T) {
	in := `{
		"first_name":"สมชาย",
		"last_name":"ใจดี",
		"nickname":"ชาย",
		"birth_date":"1990-05-20",
		"id_card_number":"1234567890123",
		"nationality":"ไทย",
		"ethnicity":"ไทย",
		"religion":"พุทธ",
		"address_detail":"123/4",
		"zip_code":"50000",
		"phone_number":"0812345678",
		"line":"@somchai",
		"salary_income":15000.5,
		"family_member_count":4,
		"agri_worker_count":2,
		"agri_experience":"2010-01-15",
		"province_id":"50",
		"district_id":"5001",
		"subdistrict_id":"500101"
	}`
	var req RegisterFarmerProfileRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.FirstName != "สมชาย" || req.LastName != "ใจดี" {
		t.Errorf("name fields: got %q %q", req.FirstName, req.LastName)
	}
	if req.SalaryIncome != 15000.5 {
		t.Errorf("salary_income: got %v", req.SalaryIncome)
	}
	if req.FamilyMemberCount != 4 {
		t.Errorf("family_member_count: got %d", req.FamilyMemberCount)
	}
}

func TestRegisterFarmRequest_GISAsPolygon(t *testing.T) {
	in := `{
		"farm_name":"ฟาร์ม A",
		"found_date":"2024-01-15",
		"subdistrict_id":"500101",
		"zip_code":"50000",
		"contact_name":"สมชาย",
		"phone_number":"0812345678",
		"gis":[
			{"lng":100.0,"lat":13.0},
			{"lng":100.1,"lat":13.0},
			{"lng":100.1,"lat":13.1},
			{"lng":100.0,"lat":13.1}
		]
	}`
	var req RegisterFarmRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.FarmName != "ฟาร์ม A" {
		t.Errorf("farm_name: got %q", req.FarmName)
	}
	if len(req.GIS) != 4 {
		t.Fatalf("GIS: want 4 points, got %d", len(req.GIS))
	}
	// buildWKT must produce a valid POLYGON for these 4 points.
	wkt := buildWKT(req.GIS)
	if !strings.HasPrefix(wkt, "POLYGON((") {
		t.Errorf("expected POLYGON, got %q", wkt)
	}
}

func TestRegisterPlotRequest_AreaConversionNote(t *testing.T) {
	// The handler converts AreaSqM / 1600 → ไร่ internally. We only test
	// that the field round-trips; the conversion is in the DB write path
	// which we don't exercise here.
	in := `{
		"farm_id":"` + uuid.New().String() + `",
		"plot_name":"แปลง 1",
		"land_ownership":"owned",
		"cocoa_planted_area":2.5,
		"has_chemical_use":true,
		"found_date":"2024-01-15",
		"gis_area_m2":1600
	}`
	var req RegisterPlotRequest
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.AreaSqM != 1600 {
		t.Errorf("area_sq_m: want 1600, got %v", req.AreaSqM)
	}
	if !req.HasChemicalUse {
		t.Error("has_chemical_use should be true")
	}
}

// --- _parseDate regression (already covered in collection test, but the
// helper is defined here; one local sanity check is fine). ----------------

func TestParseDateLocal(t *testing.T) {
	got := _parseDate("2025-12-31")
	want := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("want %v, got %v", want, got)
	}
}
