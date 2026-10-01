package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"go-server-mobile/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxFarmImageBytes = 5 << 20
	// Room for the multipart boundary/headers around the file itself, so a
	// file right at the limit isn't rejected by the body cap first.
	farmImageMultipartSlack = 1 << 20
	farmImageURLExpiry      = time.Hour

	// storage.file is polymorphic over (table_name, ref_id); code tells a
	// farm's photo apart from any other file later attached to a farm.
	farmImageTableName = "agriculture.farm"
	farmImageCode      = "farm_image"
)

// Keyed by the type sniffed from the bytes, not the client's filename or
// Content-Type header, either of which the client can set to anything.
var farmImageExtensions = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

func (h *AgricultureHandler) UploadFarmImage(c *gin.Context) {
	if h.ImageStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ระบบเก็บรูปยังไม่พร้อมใช้งาน"})
		return
	}

	val, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID := val.(uuid.UUID)

	farmID, err := uuid.Parse(c.Param("farm_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "farm_id ไม่ถูกต้อง"})
		return
	}

	var owned int64
	if err := h.DB.Table("agriculture.farmer_farm").
		Where("farmer_id = ? AND farm_id = ?", userID, farmID).
		Count(&owned).Error; err != nil {
		slog.Error("farm ownership check failed", "error", err, "farm_id", farmID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ตรวจสอบฟาร์มล้มเหลว"})
		return
	}
	if owned == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์แก้ไขฟาร์มนี้"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFarmImageBytes+farmImageMultipartSlack)
	fileHeader, err := c.FormFile("image")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "รูปต้องมีขนาดไม่เกิน 5 MB"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบไฟล์รูปในช่อง image"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "อ่านไฟล์รูปไม่ได้"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxFarmImageBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "อ่านไฟล์รูปไม่ได้"})
		return
	}
	if len(data) > maxFarmImageBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "รูปต้องมีขนาดไม่เกิน 5 MB"})
		return
	}

	contentType := http.DetectContentType(data)
	ext, allowed := farmImageExtensions[contentType]
	if !allowed {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "รองรับเฉพาะไฟล์ JPG, PNG หรือ WEBP"})
		return
	}

	objectName := fmt.Sprintf("%s.%s", uuid.New(), ext)
	key := fmt.Sprintf("farms/%s/%s", farmID, objectName)
	ctx := c.Request.Context()

	if err := h.ImageStore.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		slog.Error("farm image upload to object store failed", "error", err, "farm_id", farmID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "อัปโหลดรูปไม่สำเร็จ กรุณาลองใหม่"})
		return
	}

	sum := sha256.Sum256(data)
	fileRow := map[string]interface{}{
		"file_name":        objectName,
		"table_name":       farmImageTableName,
		"ref_id":           farmID,
		"code":             farmImageCode,
		"original_name":    truncateRunes(filepath.Base(fileHeader.Filename), 255),
		"file_extension":   ext,
		"mime_type":        contentType,
		"file_size":        len(data),
		"storage_path":     key,
		"storage_provider": "r2",
		"checksum":         hex.EncodeToString(sum[:]),
		"is_public":        false,
		"status":           "active",
		"uploaded_by":      userID,
	}
	if err := h.DB.Table("storage.file").Create(&fileRow).Error; err != nil {
		// The object is already in R2 but nothing points at it; it's
		// harmless (never listed) and cheaper to leave than to undo.
		slog.Error("failed to record farm image in storage.file", "error", err, "farm_id", farmID, "key", key)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "บันทึกข้อมูลรูปล้มเหลว"})
		return
	}

	var imageURL *string
	if u, err := h.ImageStore.PresignGet(ctx, key, farmImageURLExpiry); err == nil {
		imageURL = &u
	} else {
		slog.Error("failed to presign farm image", "error", err, "key", key)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "อัปโหลดรูปฟาร์มสำเร็จ",
		"farm_id":   farmID,
		"image_url": imageURL,
	})
}

// attachFarmImageURLs fills ImageURL with each farm's most recent photo.
// Failures only cost the photo, never the farm list itself.
func (h *AgricultureHandler) attachFarmImageURLs(ctx context.Context, farms []models.Farm) {
	if h.ImageStore == nil || len(farms) == 0 {
		return
	}

	ids := make([]uuid.UUID, len(farms))
	for i, f := range farms {
		ids[i] = f.FarmID
	}

	var rows []struct {
		RefID       uuid.UUID `gorm:"column:ref_id"`
		StoragePath string    `gorm:"column:storage_path"`
	}
	err := h.DB.Raw(`
		SELECT DISTINCT ON (ref_id) ref_id, storage_path
		FROM storage.file
		WHERE table_name = ? AND code = ? AND status = 'active' AND ref_id IN ?
		ORDER BY ref_id, created_at DESC`,
		farmImageTableName, farmImageCode, ids,
	).Scan(&rows).Error
	if err != nil {
		slog.Error("failed to load farm images", "error", err)
		return
	}

	urls := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		u, err := h.ImageStore.PresignGet(ctx, r.StoragePath, farmImageURLExpiry)
		if err != nil {
			slog.Error("failed to presign farm image", "error", err, "key", r.StoragePath)
			continue
		}
		urls[r.RefID] = u
	}
	for i := range farms {
		if u, ok := urls[farms[i].FarmID]; ok {
			farms[i].ImageURL = &u
		}
	}
}

func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
