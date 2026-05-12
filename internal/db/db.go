package db

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"showcaster-be/internal/models"
)

// New opens a SQLite connection at dbPath, runs auto-migration for all models,
// and creates the composite index required for job queue queries.
// It returns the *gorm.DB instance ready for injection into services.
func New(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", dbPath, err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.Job{}, &models.Step{}); err != nil {
		return nil, fmt.Errorf("db: auto-migrate: %w", err)
	}

	// Create composite index used by job queue polling queries.
	// IF NOT EXISTS makes this idempotent across restarts.
	if err := db.Exec(
		"CREATE INDEX IF NOT EXISTS idx_jobs_status_created_at ON jobs(status, created_at)",
	).Error; err != nil {
		return nil, fmt.Errorf("db: create index idx_jobs_status_created_at: %w", err)
	}

	return db, nil
}
