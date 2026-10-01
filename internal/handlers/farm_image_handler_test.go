package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go-server-mobile/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeObjectStore struct {
	putKeys         []string
	putContentTypes []string
	putErr          error
}

func (f *fakeObjectStore) Put(_ context.Context, key string, body io.Reader, _ int64, contentType string) error {
	if f.putErr != nil {
		return f.putErr
	}
	_, _ = io.Copy(io.Discard, body)
	f.putKeys = append(f.putKeys, key)
	f.putContentTypes = append(f.putContentTypes, contentType)
	return nil
}

func (f *fakeObjectStore) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://r2.test/" + key + "?sig=x", nil
}

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

var ownershipQuery = regexp.QuoteMeta(`SELECT count(*) FROM "agriculture"."farmer_farm"`)

func farmImageRequest(t *testing.T, farmID, field, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if field != "" {
		part, err := w.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(data)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/farms/"+farmID+"/image", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func serveFarmImage(h *AgricultureHandler, userID uuid.UUID, req *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/farms/:farm_id/image", func(c *gin.Context) { c.Set("userID", userID) }, h.UploadFarmImage)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUploadFarmImage_NoStoreConfiguredReturns503(t *testing.T) {
	h := &AgricultureHandler{}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "a.png", pngHeader))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestUploadFarmImage_InvalidFarmIDReturns400(t *testing.T) {
	h := &AgricultureHandler{ImageStore: &fakeObjectStore{}}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, "not-a-uuid", "image", "a.png", pngHeader))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestUploadFarmImage_SomeoneElsesFarmReturns403(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	store := &fakeObjectStore{}
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	h := &AgricultureHandler{DB: gdb, ImageStore: store}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "a.png", pngHeader))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
	if len(store.putKeys) != 0 {
		t.Fatal("nothing should be uploaded for a farm the caller doesn't own")
	}
}

func TestUploadFarmImage_MissingFileFieldReturns400(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{}}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "", "", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestUploadFarmImage_NonImageReturns415EvenWithImageExtension(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	store := &fakeObjectStore{}
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	h := &AgricultureHandler{DB: gdb, ImageStore: store}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "evil.png", []byte("<html><script>alert(1)</script>")))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("want 415, got %d", rec.Code)
	}
	if len(store.putKeys) != 0 {
		t.Fatal("a non-image must not reach the object store")
	}
}

func TestUploadFarmImage_FileJustOverLimitReturns413(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	data := append(append([]byte{}, pngHeader...), make([]byte, maxFarmImageBytes)...)
	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{}}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "big.png", data))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestUploadFarmImage_BodyFarOverLimitReturns413(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	data := append(append([]byte{}, pngHeader...), make([]byte, 3*maxFarmImageBytes)...)
	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{}}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "huge.png", data))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestUploadFarmImage_StoreFailureReturns502AndRecordsNothing(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{putErr: errors.New("r2 down")}}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, uuid.NewString(), "image", "a.png", pngHeader))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no storage.file row should be written when the upload failed: %v", err)
	}
}

func TestUploadFarmImage_HappyPathStoresObjectAndRow(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	store := &fakeObjectStore{}
	farmID := uuid.New()
	mock.ExpectQuery(ownershipQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "storage"."file"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	h := &AgricultureHandler{DB: gdb, ImageStore: store}
	rec := serveFarmImage(h, uuid.New(), farmImageRequest(t, farmID.String(), "image", "farm.jpg", pngHeader))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.putKeys) != 1 {
		t.Fatalf("want 1 upload, got %d", len(store.putKeys))
	}
	key := store.putKeys[0]
	// Extension and content type follow the sniffed bytes (PNG), not the
	// ".jpg" filename the client claimed.
	if !strings.HasPrefix(key, "farms/"+farmID.String()+"/") || !strings.HasSuffix(key, ".png") {
		t.Errorf("unexpected object key %q", key)
	}
	if store.putContentTypes[0] != "image/png" {
		t.Errorf("want image/png, got %q", store.putContentTypes[0])
	}

	var resp struct {
		ImageURL *string `json:"image_url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ImageURL == nil || !strings.Contains(*resp.ImageURL, key) {
		t.Errorf("response should carry a URL for the new object, got %v", resp.ImageURL)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAttachFarmImageURLs_OnlyFarmsWithAPhotoGetAURL(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	withPhoto, withoutPhoto := uuid.New(), uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON (ref_id) ref_id, storage_path`)).
		WillReturnRows(sqlmock.NewRows([]string{"ref_id", "storage_path"}).
			AddRow(withPhoto, "farms/"+withPhoto.String()+"/x.jpg"))

	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{}}
	farms := []models.Farm{{FarmID: withPhoto}, {FarmID: withoutPhoto}}
	h.attachFarmImageURLs(context.Background(), farms)

	if farms[0].ImageURL == nil || !strings.Contains(*farms[0].ImageURL, withPhoto.String()) {
		t.Errorf("farm with a photo should get its URL, got %v", farms[0].ImageURL)
	}
	if farms[1].ImageURL != nil {
		t.Errorf("farm without a photo should stay nil, got %q", *farms[1].ImageURL)
	}
}

func TestAttachFarmImageURLs_QueryFailureLeavesFarmsUntouched(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON (ref_id)`)).WillReturnError(errors.New("db down"))

	h := &AgricultureHandler{DB: gdb, ImageStore: &fakeObjectStore{}}
	farms := []models.Farm{{FarmID: uuid.New()}}
	h.attachFarmImageURLs(context.Background(), farms)

	if farms[0].ImageURL != nil {
		t.Error("a failed image lookup must not invent a URL")
	}
}

func TestAttachFarmImageURLs_NoStoreSkipsTheQuery(t *testing.T) {
	gdb, mock := newMockGormDB(t)
	h := &AgricultureHandler{DB: gdb}
	h.attachFarmImageURLs(context.Background(), []models.Farm{{FarmID: uuid.New()}})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
