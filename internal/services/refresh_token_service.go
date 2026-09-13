package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go-server-mobile/internal/models"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const defaultRefreshTokenExpirationSec = 86400 // 1 day, matches .env.sample/render.yaml

// GO-3: the refresh-token half of the flow -- see the V19 migration
// (auth.refresh_token) in the database repo for the storage shape. The
// access token (GenerateToken, above) stays exactly as it was; this is
// additive, not a replacement.

func refreshTokenExpiration() time.Duration {
	return time.Duration(RefreshTokenExpirationSeconds()) * time.Second
}

// RefreshTokenExpirationSeconds is exported so handlers can compute the
// refresh cookie's MaxAge without duplicating the env-var-with-default
// logic below.
func RefreshTokenExpirationSeconds() int {
	sec, err := strconv.Atoi(os.Getenv("JWT_REFRESH_TOKEN_EXPIRATION"))
	if err != nil || sec <= 0 {
		sec = defaultRefreshTokenExpirationSec
	}
	return sec
}

// generateRefreshTokenValue produces the raw, high-entropy token handed to
// the client. Unlike the access token, this isn't a JWT -- it's an opaque
// secret validated against the DB, since its whole point is to be
// revocable/rotatable server-side.
func generateRefreshTokenValue() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// IssueRefreshToken creates and persists a new refresh token for userID,
// returning the raw value (only ever returned here -- the DB only ever
// sees its hash).
func IssueRefreshToken(db *gorm.DB, userID uuid.UUID) (raw string, expiresAt time.Time, err error) {
	raw, err = generateRefreshTokenValue()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(refreshTokenExpiration())

	record := models.RefreshToken{
		UserID:    userID,
		TokenHash: hashRefreshToken(raw),
		ExpiresAt: expiresAt,
	}
	if err := db.Table("auth.refresh_token").Create(&record).Error; err != nil {
		return "", time.Time{}, fmt.Errorf("save refresh token: %w", err)
	}
	return raw, expiresAt, nil
}

// RotateRefreshToken redeems rawToken for a fresh one: the presented token
// is marked used (so a stolen-and-replayed copy is rejected on its next
// use) and a new one is issued, in one transaction so a failure partway
// through can't leave a token marked used with no replacement issued.
func RotateRefreshToken(db *gorm.DB, rawToken string) (newRaw string, userID uuid.UUID, err error) {
	hash := hashRefreshToken(rawToken)

	var existing models.RefreshToken
	if err := db.Table("auth.refresh_token").Where("token_hash = ?", hash).First(&existing).Error; err != nil {
		return "", uuid.Nil, fmt.Errorf("refresh token not recognized")
	}
	if existing.UsedAt != nil || existing.RevokedAt != nil {
		return "", uuid.Nil, fmt.Errorf("refresh token already used or revoked")
	}
	if time.Now().After(existing.ExpiresAt) {
		return "", uuid.Nil, fmt.Errorf("refresh token expired")
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Table("auth.refresh_token").
			Where("refresh_token_id = ?", existing.RefreshTokenID).
			Update("used_at", now).Error; err != nil {
			return err
		}

		raw, _, err := IssueRefreshToken(tx, existing.UserID)
		if err != nil {
			return err
		}
		newRaw = raw
		return nil
	})
	if err != nil {
		return "", uuid.Nil, err
	}

	return newRaw, existing.UserID, nil
}
