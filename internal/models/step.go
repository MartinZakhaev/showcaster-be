package models

import "time"

// Step represents one of the four pipeline segments of a Job.
type Step struct {
	ID        string    `gorm:"primaryKey;type:text"`
	JobID     string    `gorm:"not null;index;type:text"`
	Name      string    `gorm:"not null;type:text"` // Hook|Problem|Solution|Closure
	Status    string    `gorm:"not null;type:text;default:'pending'"` // pending|processing|completed|failed
	VideoURL  string    `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
