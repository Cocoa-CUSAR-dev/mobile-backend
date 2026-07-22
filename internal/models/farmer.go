package models

import (
	"time"
	"github.com/google/uuid"
)
type Farmer struct {
	UserID            uuid.UUID `gorm:"primaryKey;column:user_id"`
	FirstName         string    `gorm:"column:first_name"`
	LastName          string    `gorm:"column:last_name"`
	Nickname          string    `gorm:"column:nickname"`
	BirthDate         time.Time `gorm:"column:birth_date"`
	IdCardNumber      string    `gorm:"column:id_card_number"`
	Nationality       string    `gorm:"column:nationality"`
	Ethnicity         string    `gorm:"column:ethnicity"`
	Religion          string    `gorm:"column:religion"`
	AddressDetail     string    `gorm:"column:address_detail"`
	SubdistrictID     uuid.UUID `gorm:"column:subdistrict_id"`
	DistrictID        uuid.UUID `gorm:"column:district_id"`
	ProvinceID        uuid.UUID `gorm:"column:province_id"`
	ZipCode           string    `gorm:"column:zip_code"`
	PhoneNumber       string    `gorm:"column:phone_number"`
	Line              string    `gorm:"column:line"`
	SalaryIncome      float64   `gorm:"column:salary_income"`
	FamilyMemberCount int       `gorm:"column:family_member_count"`
	AgriWorkerCount   int       `gorm:"column:agri_worker_count"`
	AgriExperience    time.Time `gorm:"column:agri_experience"`
	CreatedAt         time.Time `gorm:"column:created_at;default:now()"`
	UpdatedAt         time.Time `gorm:"column:updated_at;default:now()"`
}

// ระบุชื่อตารางพร้อม Schema ให้ GORM
func (Farmer) TableName() string {
	return "agriculture.farmer"
}