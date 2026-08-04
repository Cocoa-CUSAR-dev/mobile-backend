package models

import (
	"time"

	"github.com/google/uuid"
)

type Farmer struct {
	UserID            uuid.UUID  `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`
	FirstName         *string    `gorm:"column:first_name" json:"first_name"`
	LastName          *string    `gorm:"column:last_name" json:"last_name"`
	Nickname          *string    `gorm:"column:nickname" json:"nickname"`
	BirthDate         *time.Time `gorm:"column:birth_date" json:"birth_date"`
	Nationality       *string    `gorm:"column:nationality" json:"nationality"`
	Ethnicity         *string    `gorm:"column:ethnicity" json:"ethnicity"`
	Religion          *string    `gorm:"column:religion" json:"religion"`
	IdCardNumber      *string    `gorm:"column:id_card_number" json:"id_card_number"`
	AddressDetail     *string    `gorm:"column:address_detail" json:"address_detail"`
	SubdistrictID     uuid.UUID  `gorm:"type:uuid;column:subdistrict_id;not null" json:"subdistrict_id"`
	DistrictID        uuid.UUID  `gorm:"type:uuid;column:district_id;not null" json:"district_id"`
	ProvinceID        uuid.UUID  `gorm:"type:uuid;column:province_id;not null" json:"province_id"`
	ZipCode           *string    `gorm:"column:zip_code" json:"zip_code"`
	PhoneNumber       *string    `gorm:"column:phone_number" json:"phone_number"`
	Line              *string    `gorm:"column:line" json:"line"`
	Facebook          *string    `gorm:"column:facebook" json:"facebook"`
	SalaryIncome      *float64   `gorm:"column:salary_income" json:"salary_income"`
	FamilyMemberCount *int       `gorm:"column:family_member_count;default:1" json:"family_member_count"`
	AgriWorkerCount   *int       `gorm:"column:agri_worker_count;default:1" json:"agri_worker_count"`
	AgriExperience    *time.Time `gorm:"column:agri_experience;default:CURRENT_DATE" json:"agri_experience"`
	CreatedAt         time.Time  `gorm:"column:created_at;default:now();not null" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;default:now();not null" json:"updated_at"`
}

func (Farmer) TableName() string {
	return "agriculture.farmer"
}
