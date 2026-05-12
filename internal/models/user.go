package models

import "time"

// User represents a registered account.
type User struct {
	ID           string    `gorm:"primaryKey;type:text"`
	Email        string    `gorm:"uniqueIndex;not null;type:text"`
	FullName     string    `gorm:"not null;type:text"`
	PasswordHash string    `gorm:"not null;type:text"`
	IsVerified   bool      `gorm:"not null;default:false"`
	OTPCode      string    `gorm:"type:text"`
	OTPExpiresAt time.Time
	OTPFailCount int       `gorm:"not null;default:0"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
