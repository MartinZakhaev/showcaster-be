package models

import "time"

// Job represents a single video generation request.
type Job struct {
	ID              string    `gorm:"primaryKey;type:text"`
	UserID          string    `gorm:"not null;index;type:text"`
	Status          string    `gorm:"not null;type:text;default:'pending'"` // pending|processing|completed|failed
	Progress        int       `gorm:"not null;default:0"`
	ModelImageURL   string    `gorm:"not null;type:text"`
	ProductImageURL string    `gorm:"not null;type:text"`
	ProductName     string    `gorm:"not null;type:text"`
	ProductCategory string    `gorm:"not null;type:text"`
	TargetAudience  string    `gorm:"not null;type:text"`
	Orientation     string    `gorm:"not null;type:text"`
	Resolution      string    `gorm:"not null;type:text"`
	ThumbnailURL    string    `gorm:"type:text"`
	DrivingAudioURL string    `gorm:"column:driving_audio_url;default:''" json:"drivingAudioUrl,omitempty"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Steps           []Step    `gorm:"foreignKey:JobID;constraint:OnDelete:CASCADE"`
	User            User      `gorm:"foreignKey:UserID"`
}
