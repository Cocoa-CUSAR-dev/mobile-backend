package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// points env vars at a test server; t.Setenv auto-restores after the test.
func withFormSchemaEnv(t *testing.T, url, key string) {
	t.Helper()
	t.Setenv("WEB_BACKEND_URL", url)
	t.Setenv("KOTLIN_SERVICE_KEY", key)
}

func TestFetchFormSchema_SendsServiceKeyAndParsesResponse(t *testing.T) {
	formID := uuid.New()
	var gotKey string
	var hits int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotKey = r.Header.Get("X-Service-Key")
		if r.URL.Path != "/service/forms/"+formID.String() {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"value":{"sections":[{"questions":[{"fieldName":"note","inputType":"VARCHAR","isMandatory":true}]}]},"error":null}`)
	}))
	defer server.Close()

	withFormSchemaEnv(t, server.URL, "test-secret")

	schema, err := fetchFormSchema(formID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "test-secret" {
		t.Errorf("want X-Service-Key %q, got %q", "test-secret", gotKey)
	}
	if len(schema.Sections) != 1 || len(schema.Sections[0].Questions) != 1 {
		t.Fatalf("schema not decoded as expected: %+v", schema)
	}
	if schema.Sections[0].Questions[0].FieldName != "note" {
		t.Errorf("want fieldName note, got %q", schema.Sections[0].Questions[0].FieldName)
	}

	// second call should hit the cache, not Kotlin again
	if _, err := fetchFormSchema(formID); err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if hits != 1 {
		t.Errorf("want 1 request to web-backend (second call cached), got %d", hits)
	}
}

func TestFetchFormSchema_UpstreamErrorEnvelope(t *testing.T) {
	formID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"value":null,"error":"form not found"}`)
	}))
	defer server.Close()

	withFormSchemaEnv(t, server.URL, "test-secret")

	if _, err := fetchFormSchema(formID); err == nil {
		t.Fatal("want error when web-backend returns an error envelope, got nil")
	}
}

func TestFetchFormSchema_NonOKStatus(t *testing.T) {
	formID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	withFormSchemaEnv(t, server.URL, "test-secret")

	if _, err := fetchFormSchema(formID); err == nil {
		t.Fatal("want error on non-200 status, got nil")
	}
}

func TestFetchFormSchema_MissingWebBackendURL(t *testing.T) {
	withFormSchemaEnv(t, "", "test-secret")

	if _, err := fetchFormSchema(uuid.New()); err == nil {
		t.Fatal("want error when WEB_BACKEND_URL is unset, got nil")
	}
}

func TestFetchFormSchema_MissingServiceKey(t *testing.T) {
	withFormSchemaEnv(t, "http://example.invalid", "")

	if _, err := fetchFormSchema(uuid.New()); err == nil {
		t.Fatal("want error when KOTLIN_SERVICE_KEY is unset, got nil")
	}
}
