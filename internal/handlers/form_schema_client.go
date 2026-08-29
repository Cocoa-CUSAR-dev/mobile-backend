package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"go-server-mobile/internal/validation"

	"github.com/google/uuid"
)

// 5 min, not forever like columnsCache — forms can be edited live via
// PUT /forms/{formId}/edit and we don't know if that bumps version.
const formSchemaCacheTTL = 5 * time.Minute

type formSchemaCacheEntry struct {
	schema    validation.FormSchema
	fetchedAt time.Time
}

// form_id -> formSchemaCacheEntry. In-process only, cleared on restart.
var formSchemaCache sync.Map

// fetchFormSchema gets a form's schema for answer validation (#54).
// Uses Kotlin's service-key route (same one the chatbot already calls,
// see chatbot/src/forms/client.py) instead of the farmer-JWT one GetTaskForm
// uses above, since SubmitTaskForUser has no farmer JWT to forward. Needs
// KOTLIN_SERVICE_KEY set to the same value as web-backend's
// CHATBOT_SERVICE_KEY — no code change needed on that side.
func fetchFormSchema(formID uuid.UUID) (validation.FormSchema, error) {
	key := formID.String()
	if cached, ok := formSchemaCache.Load(key); ok {
		entry := cached.(formSchemaCacheEntry)
		if time.Since(entry.fetchedAt) < formSchemaCacheTTL {
			return entry.schema, nil
		}
	}

	webBackendURL := os.Getenv("WEB_BACKEND_URL")
	if webBackendURL == "" {
		return validation.FormSchema{}, fmt.Errorf("ยังไม่ได้ตั้งค่า WEB_BACKEND_URL")
	}
	serviceKey := os.Getenv("KOTLIN_SERVICE_KEY")
	if serviceKey == "" {
		return validation.FormSchema{}, fmt.Errorf("ยังไม่ได้ตั้งค่า KOTLIN_SERVICE_KEY")
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/service/forms/%s", webBackendURL, key), nil)
	if err != nil {
		return validation.FormSchema{}, fmt.Errorf("สร้างคำขอไปยังระบบฟอร์มไม่สำเร็จ: %w", err)
	}
	req.Header.Set("X-Service-Key", serviceKey)

	resp, err := webBackendClient.Do(req)
	if err != nil {
		return validation.FormSchema{}, fmt.Errorf("ไม่สามารถติดต่อระบบฟอร์มได้: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return validation.FormSchema{}, fmt.Errorf("อ่านข้อมูลฟอร์มจากระบบฟอร์มไม่สำเร็จ: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return validation.FormSchema{}, fmt.Errorf("ระบบฟอร์มตอบกลับสถานะ %d", resp.StatusCode)
	}

	// Same {value, error} envelope GetTaskForm above already unwraps.
	var kotlinResp struct {
		Value validation.FormSchema `json:"value"`
		Error *string               `json:"error"`
	}
	if err := json.Unmarshal(body, &kotlinResp); err != nil {
		return validation.FormSchema{}, fmt.Errorf("แปลงข้อมูลฟอร์มไม่สำเร็จ: %w", err)
	}
	if kotlinResp.Error != nil {
		return validation.FormSchema{}, fmt.Errorf("ระบบฟอร์มส่ง error กลับมา: %s", *kotlinResp.Error)
	}

	formSchemaCache.Store(key, formSchemaCacheEntry{schema: kotlinResp.Value, fetchedAt: time.Now()})
	return kotlinResp.Value, nil
}
