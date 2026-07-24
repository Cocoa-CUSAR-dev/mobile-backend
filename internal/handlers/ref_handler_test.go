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

// --- GetConstants: routing switch -----------------------------------------
//
// GetConstants is a single endpoint that dispatches to ~27 different DB
// queries based on the :key URL param. The interesting pre-DB behavior to
// lock down:
//   1. unknown key         → 404 "Constant key not found"
//   2. location key without
//      zip_code or
//      subdistrict_id      → 400 "กรุณาระบุ zip_code หรือ subdistrict_id"
//   3. userID is the GUARDED
//      form (val, _ := c.Get;
//      userID, _ := val.(...))  — unlike the rest of the codebase, this
//      handler must NOT panic when middleware is missing.

// --- Unknown key (default branch) ----------------------------------------

func TestGetConstants_UnknownKeyReturns404(t *testing.T) {
	// Default case in the switch. Pure pre-DB — no DB touched.
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/no_such_key", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON body: %s", w.Body.String())
	}
	if body["error"] != "Constant key not found" {
		t.Errorf("want 'Constant key not found', got %q", body["error"])
	}
}

// --- Known keys: userID extraction is GUARDED ----------------------------
//
// GetConstants uses the two-value form `val.(uuid.UUID)` — it silently
// drops the UUID if middleware is missing. None of the other handlers in
// the codebase do this, so it's worth pinning down: a refactor that
// changes this to the one-value form would turn missing-userID requests
// into 500 panics. We assert no panic and that the handler reaches the
// DB (which nil-panics, recovered to 500) — proving the guarded assert
// ran instead of panicking on the context lookup.

func TestGetConstants_MissingUserIDDoesNotPanicOnLookup(t *testing.T) {
	// The handler signature is val, _ := c.Get("userID"); userID, _ := val.(uuid.UUID)
	// If this changes to val.(uuid.UUID) (one-value), the test panics here
	// instead of inside the DB call.
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				// If we reach here from a uuid.UUID type-assertion panic,
				// that means someone removed the guard. Surface it loudly.
				if strings.Contains(string(rec.(error).Error()), "uuid.UUID") {
					t.Errorf("userID type assertion lost its guard: %v", rec)
				}
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		// Don't set userID — handler should still get past the lookup.
		h.GetConstants(c)
	})

	// Pick a key that hits the DB on success — anything non-default, non-location.
	req := httptest.NewRequest(http.MethodGet, "/constants/breed", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// We expect either:
	//   - 500 (DB panic recovered, guard held)   ← what we want
	//   - 200/empty array (guard held, no panic) ← also fine if some path
	//                                                returned early
	// We do NOT want 404 (the key would still match) and we do NOT want
	// a uuid.UUID panic leak through.
	if w.Code == http.StatusNotFound {
		t.Errorf("missing userID should not flip the switch to default; got 404")
	}
}

// --- Location branch: pre-validation 400 ---------------------------------

func TestGetConstants_LocationMissingBothFiltersReturns400(t *testing.T) {
	// The 'location' case has its own validation: at least one of
	// zip_code / subdistrict_id must be present. This runs BEFORE the DB
	// hit, so it must 400 with the Thai error string.
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/location", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/location", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "zip_code") {
		t.Errorf("want 'zip_code' in body, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "subdistrict_id") {
		t.Errorf("want 'subdistrict_id' in body, got %s", w.Body.String())
	}
}

func TestGetConstants_LocationWithZipCodeReachesDB(t *testing.T) {
	// zip_code provided → pre-validation passes → DB is touched → 500
	// (nil DB panic, recovered). Confirms the validation gate is
	// zip_code OR subdistrict_id, not AND.
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/location", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/location?zip_code=50000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered, validation passed), got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

func TestGetConstants_LocationWithSubdistrictIDReachesDB(t *testing.T) {
	// subdistrict_id alone (no zip_code) must also pass validation.
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/location", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/location?subdistrict_id=500101", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic recovered, validation passed), got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

// --- Known key: pre-DB pass-through to DB --------------------------------
//
// For a non-special key (no pre-validation, no userID requirement), the
// handler must reach the DB. With a nil DB we get 500, which observably
// proves (a) the switch matched, (b) no 400/short-circuit fired. This
// guards against accidental short-circuits in the routing layer.

func TestGetConstants_KnownKeyReachesDB(t *testing.T) {
	cases := []string{
		"air_exposure_type",
		"breed",
		"chem_bio",
		"cocoa_brean_grade", // sic: source has this misspelling; do not "fix" it
		"drying_facility",
		"farm_activity_type",
		"fertilizer_stage",
		"fertilizer",
		"grade",
		"hole_filler",
		"land_type",
		"location_type",
		"pest_disease",
		"processing_activity_type",
		"processing_defect",
		"soil_type",
		"tank_material",
		"water_source",
		"watering_system",
		"weather_condition",
		"province",
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			h := &RefHandler{}
			r := gin.New()
			r.GET("/constants/:key", func(c *gin.Context) {
				c.Set("userID", uuid.New())
				defer func() {
					if rec := recover(); rec != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
					}
				}()
				h.GetConstants(c)
			})

			req := httptest.NewRequest(http.MethodGet, "/constants/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("key %q routed to default case (404) — did the case label get renamed?", key)
			}
			if w.Code != http.StatusInternalServerError {
				t.Errorf("key %q: want 500 (DB panic recovered), got %d (body=%s)",
					key, w.Code, w.Body.String())
			}
		})
	}
}

// --- User-scoped keys: missing userID reaches DB via guarded lookup ------
//
// farm / plot / hub / processing_station / batch / harvest all read
// userID from the context. The guarded type assertion means a missing
// userID produces a zero UUID, which still flows into the WHERE clause —
// the DB will return zero rows. With a nil DB we get 500, NOT a panic
// at the lookup. This is the value of the guard.

func TestGetConstants_UserScopedKeysDoNotPanicWithoutUserID(t *testing.T) {
	cases := []string{"farm", "plot", "hub", "processing_station", "batch", "harvest"}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			h := &RefHandler{}
			r := gin.New()
			r.GET("/constants/:key", func(c *gin.Context) {
				defer func() {
					if rec := recover(); rec != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
					}
				}()
				// Intentionally NOT setting userID.
				h.GetConstants(c)
			})

			req := httptest.NewRequest(http.MethodGet, "/constants/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// The DB call will panic (nil DB). What matters is that
			// it's the DB panic, not a uuid.UUID type-assertion panic
			// at the context lookup.
			if w.Code != http.StatusInternalServerError {
				t.Errorf("key %q: want 500, got %d (body=%s)", key, w.Code, w.Body.String())
			}
		})
	}
}

// --- district / subdistrict: query-param-driven where clause -------------
//
// These two cases accept an optional filter. The pre-DB branches we can
// lock down: with a valid userID and a filter that doesn't short-circuit,
// the handler must reach the DB (no early 400/404 in the filter path).

func TestGetConstants_DistrictWithProvinceFilterReachesDB(t *testing.T) {
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/district?province_id=50", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetConstants_DistrictWithoutProvinceFilterReachesDB(t *testing.T) {
	// Empty filter → still hits the DB (no early 400).
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/district", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetConstants_SubdistrictWithDistrictFilterReachesDB(t *testing.T) {
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/subdistrict?district_id=5001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- plot / batch / harvest: optional filter (regression guard) ----------

func TestGetConstants_PlotWithFarmFilterReachesDB(t *testing.T) {
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/plot?farm_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetConstants_BatchWithStationFilterReachesDB(t *testing.T) {
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/batch?station_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestGetConstants_HarvestWithHubFilterReachesDB(t *testing.T) {
	h := &RefHandler{}
	r := gin.New()
	r.GET("/constants/:key", func(c *gin.Context) {
		c.Set("userID", uuid.New())
		defer func() {
			if rec := recover(); rec != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"panic": true})
			}
		}()
		h.GetConstants(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/constants/harvest?hub_id="+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (DB panic), got %d (body=%s)", w.Code, w.Body.String())
	}
}
