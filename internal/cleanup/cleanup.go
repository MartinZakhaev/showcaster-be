package cleanup

import (
	"log/slog"
	"time"

	"gorm.io/gorm"

	"showcaster-be/internal/models"
)

// StartCleanupRoutine starts a background goroutine that runs the cleanup
// logic once every 24 hours. It is non-blocking and runs for the lifetime
// of the process.
func StartCleanupRoutine(db *gorm.DB, logger *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		for range ticker.C {
			runCleanup(db, logger)
		}
	}()
}

// runCleanup deletes all steps and jobs whose status is 'completed' or
// 'failed' and whose created_at is older than 120 hours (5 days). It runs
// inside a single GORM transaction so that a mid-run DB failure rolls back
// any partial deletions. Errors are logged and the routine returns without
// crashing, resuming on the next scheduled tick.
func runCleanup(db *gorm.DB, logger *slog.Logger) {
	cutoff := time.Now().Add(-120 * time.Hour)

	var deletedCount int64

	err := db.Transaction(func(tx *gorm.DB) error {
		// Delete steps belonging to expired jobs first to satisfy the
		// foreign-key constraint (steps.job_id → jobs.id).
		if err := tx.
			Where("job_id IN (?)",
				tx.Model(&models.Job{}).
					Select("id").
					Where("status IN (?) AND created_at < ?",
						[]string{"completed", "failed"}, cutoff),
			).
			Delete(&models.Step{}).Error; err != nil {
			return err
		}

		// Delete the expired jobs and capture the row count.
		result := tx.
			Where("status IN (?) AND created_at < ?",
				[]string{"completed", "failed"}, cutoff).
			Delete(&models.Job{})
		if result.Error != nil {
			return result.Error
		}

		deletedCount = result.RowsAffected
		return nil
	})

	if err != nil {
		logger.Error("cleanup: db error", "error", err)
		return
	}

	logger.Info("cleanup: deleted jobs", "count", deletedCount)
}
