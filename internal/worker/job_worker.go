package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"showcaster-be/internal/clients"
	"showcaster-be/internal/models"
)

// stepOrder defines the fixed processing sequence for a Job's pipeline.
var stepOrder = []string{"Hook", "Problem", "Solution", "Closure"}

// JobWorker reads Jobs from a channel and drives the 4-step video generation
// pipeline (Hook → Problem → Solution → Closure) for each job.
type JobWorker struct {
	queue      <-chan models.Job
	db         *gorm.DB
	replicate  clients.ReplicateClient
	cloudinary clients.CloudinaryClient
	logger     *slog.Logger

	// retrySleep controls the delay between retry attempts. Defaults to 30s;
	// can be overridden in tests to speed up execution.
	retrySleep time.Duration
}

// NewJobWorker constructs a JobWorker with the given dependencies.
// retrySleep defaults to 30 seconds.
func NewJobWorker(
	queue <-chan models.Job,
	db *gorm.DB,
	replicate clients.ReplicateClient,
	cloudinary clients.CloudinaryClient,
	logger *slog.Logger,
) *JobWorker {
	return &JobWorker{
		queue:      queue,
		db:         db,
		replicate:  replicate,
		cloudinary: cloudinary,
		logger:     logger,
		retrySleep: 30 * time.Second,
	}
}

// Run reads Jobs from the queue and processes them one at a time until the
// channel is closed or the context is cancelled.
func (w *JobWorker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-w.queue:
			if !ok {
				// Channel closed — worker shuts down gracefully.
				return
			}
			w.processJob(ctx, job)
		}
	}
}

// processJob drives the full pipeline for a single job. A recover() wrapper
// ensures that a panic inside the pipeline does not crash the worker goroutine;
// instead the job is marked failed and the worker continues.
func (w *JobWorker) processJob(ctx context.Context, job models.Job) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("job_worker: panic recovered",
				slog.String("job_id", job.ID),
				slog.Any("panic", r),
			)
			w.markJobFailed(job.ID)
		}
	}()

	// Mark the job as processing.
	if err := w.db.Model(&models.Job{}).
		Where("id = ?", job.ID).
		Update("status", "processing").Error; err != nil {
		w.logger.Error("job_worker: failed to set job processing",
			slog.String("job_id", job.ID),
			slog.Any("error", err),
		)
		return
	}

	// Load the job's steps from the DB so we have their IDs.
	var steps []models.Step
	if err := w.db.
		Where("job_id = ?", job.ID).
		Find(&steps).Error; err != nil {
		w.logger.Error("job_worker: failed to load steps",
			slog.String("job_id", job.ID),
			slog.Any("error", err),
		)
		w.markJobFailed(job.ID)
		return
	}

	// Build a name → step map for ordered lookup.
	stepByName := make(map[string]models.Step, len(steps))
	for _, s := range steps {
		stepByName[s.Name] = s
	}

	completedCount := 0

	for _, stepName := range stepOrder {
		// Check if the job was cancelled before starting each step.
		var currentJob models.Job
		if err := w.db.Select("status").First(&currentJob, "id = ?", job.ID).Error; err == nil {
			if currentJob.Status == "cancelled" {
				w.logger.Info("job_worker: job cancelled, stopping pipeline",
					slog.String("job_id", job.ID),
					slog.String("at_step", stepName),
				)
				return
			}
		}
		step, ok := stepByName[stepName]
		if !ok {
			w.logger.Error("job_worker: step not found",
				slog.String("job_id", job.ID),
				slog.String("step", stepName),
			)
			w.markJobFailed(job.ID)
			return
		}

		// Mark the step as processing.
		if err := w.db.Model(&models.Step{}).
			Where("id = ?", step.ID).
			Update("status", "processing").Error; err != nil {
			w.logger.Error("job_worker: failed to set step processing",
				slog.String("job_id", job.ID),
				slog.String("step", stepName),
				slog.Any("error", err),
			)
			w.markJobFailed(job.ID)
			return
		}

		// Attempt the step up to 3 times.
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			videoURL, cloudinaryURL, err := w.executeStep(ctx, job, stepName)
			if err == nil {
				// Success — atomically update step and job progress.
				completedCount++
				progress := (completedCount * 100) / 4

				if dbErr := w.db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(&models.Step{}).
						Where("id = ?", step.ID).
						Updates(map[string]interface{}{
							"status":    "completed",
							"video_url": cloudinaryURL,
						}).Error; err != nil {
						return fmt.Errorf("update step: %w", err)
					}
					if err := tx.Model(&models.Job{}).
						Where("id = ?", job.ID).
						Update("progress", progress).Error; err != nil {
						return fmt.Errorf("update job progress: %w", err)
					}
					return nil
				}); dbErr != nil {
					w.logger.Error("job_worker: failed to persist step completion",
						slog.String("job_id", job.ID),
						slog.String("step", stepName),
						slog.Any("error", dbErr),
					)
					w.markJobFailed(job.ID)
					return
				}

				_ = videoURL // raw Replicate URL no longer needed after upload
				lastErr = nil
				break
			}

			lastErr = err
			w.logger.Warn("job_worker: step attempt failed",
				slog.String("job_id", job.ID),
				slog.String("step", stepName),
				slog.Int("attempt", attempt),
				slog.Any("error", err),
			)

			if attempt < 3 {
				time.Sleep(w.retrySleep)
			}
		}

		if lastErr != nil {
			// All 3 attempts exhausted — mark step and job as failed.
			w.logger.Error("job_worker: step failed after 3 attempts",
				slog.String("job_id", job.ID),
				slog.String("step", stepName),
				slog.Any("error", lastErr),
			)

			_ = w.db.Model(&models.Step{}).
				Where("id = ?", step.ID).
				Update("status", "failed").Error

			w.markJobFailed(job.ID)
			return
		}
	}

	// All 4 steps completed successfully.
	if err := w.db.Model(&models.Job{}).
		Where("id = ?", job.ID).
		Update("status", "completed").Error; err != nil {
		w.logger.Error("job_worker: failed to set job completed",
			slog.String("job_id", job.ID),
			slog.Any("error", err),
		)
	}
}

// executeStep calls the Replicate client to generate a video clip and then
// uploads it to Cloudinary. It returns the raw Replicate URL and the
// Cloudinary URL on success, or an error if either operation fails.
func (w *JobWorker) executeStep(ctx context.Context, job models.Job, stepName string) (replicateURL, cloudinaryURL string, err error) {
	replicateURL, err = w.replicate.Generate(ctx, job, stepName)
	if err != nil {
		return "", "", fmt.Errorf("replicate generate: %w", err)
	}

	cloudinaryURL, err = w.cloudinary.UploadVideoFromURL(ctx, replicateURL)
	if err != nil {
		return "", "", fmt.Errorf("cloudinary upload: %w", err)
	}

	return replicateURL, cloudinaryURL, nil
}

// markJobFailed sets the job's status to "failed" in the DB. Errors are
// logged but not propagated — the caller has already decided to abort.
func (w *JobWorker) markJobFailed(jobID string) {
	if err := w.db.Model(&models.Job{}).
		Where("id = ?", jobID).
		Update("status", "failed").Error; err != nil {
		w.logger.Error("job_worker: failed to mark job as failed",
			slog.String("job_id", jobID),
			slog.Any("error", err),
		)
	}
}
