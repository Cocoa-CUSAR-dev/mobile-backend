package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

// GO-6: shared page/page_size parsing and limit/offset math so every list
// handler applying pagination agrees on the same defaults and bounds,
// instead of each one hand-rolling `page * size`.
func paginationParams(c *gin.Context) (page, size int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "0"))
	if err != nil || page < 0 {
		page = 0
	}
	size, err = strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultPageSize)))
	if err != nil || size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size
}

// Paginate is a GORM scope: db.Scopes(Paginate(c)).Find(&results).
// Callers still need their own stable Order() -- Limit/Offset without one
// can return a row twice or skip one across pages.
func Paginate(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	page, size := paginationParams(c)
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset(page * size).Limit(size)
	}
}
