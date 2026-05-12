package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"showcaster-be/internal/dto"
	"showcaster-be/internal/models"
)

// Job-specific sentinel errors returned by JobService methods.
// Handlers map these to HTTP status codes so the service layer stays HTTP-agnostic.
var (
	// ErrJobNotFound is returned when the requested job does not exist in the DB.
	ErrJobNotFound = errors.New("job not found")

	// ErrJobForbidden is returned when the authenticated user does not own the job.
	ErrJobForbidden = errors.New("access to this job is forbidden")

	// ErrJobConflict is returned when a delete is attempted on a pending/processing job.
	ErrJobConflict = errors.New("job cannot be deleted while pending or processing")

	// ErrQueueFull is returned when the job queue channel is full and the send would block.
	ErrQueueFull = errors.New("service temporarily unavailable: job queue is full")
)

// Note: ErrDBUnavailable is already defined in auth_service.go and shared across the package.

// JobService defines the contract for job CRUD and enqueuing operations.
type JobService interface {
	// CreateJob atomically creates a Job and 4 Steps in the DB, then enqueues the job.
	// Returns ErrQueueFull if the channel is full, ErrDBUnavailable on DB errors.
	CreateJob(ctx context.Context, userID string, req dto.CreateJobRequest) (dto.CreateJobResponse, error)

	// GetJob fetches a job with its steps by ID.
	// Returns ErrJobNotFound if the job does not exist, ErrJobForbidden on ownership mismatch.
	GetJob(ctx context.Context, userID, jobID string) (dto.JobResponse, error)

	// ListJobs returns a paginated list of jobs belonging to the authenticated user,
	// ordered by createdAt DESC.
	ListJobs(ctx context.Context, userID string, params dto.PaginationParams) (dto.JobListResponse, error)

	// DeleteJob atomically deletes a job and its steps.
	// Returns ErrJobNotFound, ErrJobForbidden, or ErrJobConflict as appropriate.
	DeleteJob(ctx context.Context, userID, jobID string) error
}

// jobService is the concrete implementation of JobService.
type jobService struct {
	db     *gorm.DB
	queue  chan<- models.Job
	logger *slog.Logger
}

// NewJobService constructs a JobService backed by the given GORM DB and job queue channel.
func NewJobService(db *gorm.DB, queue chan<- models.Job, logger *slog.Logger) JobService {
	return &jobService{
		db:     db,
		queue:  queue,
		logger: logger,
	}
}

// ─── CreateJob ───────────────────────────────────────────────────────────────

// CreateJob runs a GORM transaction that inserts one Job row (status=pending) and
// four Step rows (Hook, Problem, Solution, Closure, each status=pending) atomically.
// After a successful commit it performs a non-blocking send to the job queue.
// Returns ErrQueueFull if the channel is full (requirement 6.8).
func (s *jobService) CreateJob(ctx context.Context, userID string, req dto.CreateJobRequest) (dto.CreateJobResponse, error) {
	jobID := uuid.New().String()

	job := models.Job{
		ID:              jobID,
		UserID:          userID,
		Status:          "pending",
		Progress:        0,
		ModelImageURL:   req.ModelImageURL,
		ProductImageURL: req.ProductImageURL,
		ProductName:     req.ProductName,
		ProductCategory: req.ProductCategory,
		TargetAudience:  req.TargetAudience,
		Orientation:     req.Orientation,
		Resolution:      req.Resolution,
	}

	stepNames := []string{"Hook", "Problem", "Solution", "Closure"}
	steps := make([]models.Step, 0, len(stepNames))
	for _, name := range stepNames {
		steps = append(steps, models.Step{
			ID:     uuid.New().String(),
			JobID:  jobID,
			Name:   name,
			Status: "pending",
		})
	}

	// Atomically insert Job + 4 Steps (requirement 6.1).
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&job).Error; err != nil {
			s.logger.Error("create_job: insert job failed", "error", err)
			return err
		}
		for i := range steps {
			if err := tx.Create(&steps[i]).Error; err != nil {
				s.logger.Error("create_job: insert step failed", "step", steps[i].Name, "error", err)
				return err
			}
		}
		return nil
	})
	if err != nil {
		return dto.CreateJobResponse{}, ErrDBUnavailable
	}

	// Non-blocking enqueue (requirement 6.8).
	select {
	case s.queue <- job:
	default:
		s.logger.Warn("create_job: job queue is full", "job_id", jobID)
		return dto.CreateJobResponse{}, ErrQueueFull
	}

	return dto.CreateJobResponse{JobID: jobID}, nil
}

// ─── GetJob ──────────────────────────────────────────────────────────────────

// GetJob fetches the job with its steps by ID, enforces ownership, and maps
// VideoURL to nil for pending/processing steps (requirements 9.1–9.5).
func (s *jobService) GetJob(ctx context.Context, userID, jobID string) (dto.JobResponse, error) {
	var job models.Job
	err := s.db.WithContext(ctx).
		Preload("Steps").
		First(&job, "id = ?", jobID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.JobResponse{}, ErrJobNotFound
		}
		s.logger.Error("get_job: db lookup failed", "job_id", jobID, "error", err)
		return dto.JobResponse{}, ErrDBUnavailable
	}

	// Ownership check (requirement 9.3).
	if job.UserID != userID {
		return dto.JobResponse{}, ErrJobForbidden
	}

	// Map steps to DTOs, nulling out VideoURL for pending/processing (requirement 9.4).
	stepResponses := make([]dto.StepResponse, 0, len(job.Steps))
	for _, step := range job.Steps {
		var videoURL *string
		if step.Status != "pending" && step.Status != "processing" && step.VideoURL != "" {
			url := step.VideoURL
			videoURL = &url
		}
		stepResponses = append(stepResponses, dto.StepResponse{
			Name:     step.Name,
			Status:   step.Status,
			VideoURL: videoURL,
		})
	}

	return dto.JobResponse{
		ID:        job.ID,
		Status:    job.Status,
		Progress:  job.Progress,
		Steps:     stepResponses,
		CreatedAt: job.CreatedAt,
	}, nil
}

// ─── ListJobs ────────────────────────────────────────────────────────────────

// ListJobs queries jobs by userID ordered by createdAt DESC with page/limit
// pagination. Default limit is 100 (requirements 10.1–10.4).
func (s *jobService) ListJobs(ctx context.Context, userID string, params dto.PaginationParams) (dto.JobListResponse, error) {
	// Apply defaults.
	page := params.Page
	limit := params.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 100
	}

	var total int64
	baseQuery := s.db.WithContext(ctx).Model(&models.Job{}).Where("user_id = ?", userID)

	if err := baseQuery.Count(&total).Error; err != nil {
		s.logger.Error("list_jobs: count failed", "user_id", userID, "error", err)
		return dto.JobListResponse{}, ErrDBUnavailable
	}

	offset := (page - 1) * limit

	var jobs []models.Job
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&jobs).Error; err != nil {
		s.logger.Error("list_jobs: query failed", "user_id", userID, "error", err)
		return dto.JobListResponse{}, ErrDBUnavailable
	}

	summaries := make([]dto.JobSummary, 0, len(jobs))
	for _, job := range jobs {
		var thumbnailURL *string
		if job.ThumbnailURL != "" {
			url := job.ThumbnailURL
			thumbnailURL = &url
		}
		summaries = append(summaries, dto.JobSummary{
			ID:           job.ID,
			Status:       job.Status,
			CreatedAt:    job.CreatedAt,
			ThumbnailURL: thumbnailURL,
		})
	}

	return dto.JobListResponse{
		Jobs:  summaries,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

// ─── DeleteJob ───────────────────────────────────────────────────────────────

// DeleteJob enforces ownership and status constraints, then atomically deletes
// the job and all associated steps in a transaction (requirements 11.1–11.5).
func (s *jobService) DeleteJob(ctx context.Context, userID, jobID string) error {
	var job models.Job
	err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrJobNotFound
		}
		s.logger.Error("delete_job: db lookup failed", "job_id", jobID, "error", err)
		return ErrDBUnavailable
	}

	// Ownership check (requirement 11.3).
	if job.UserID != userID {
		return ErrJobForbidden
	}

	// Status constraint: cannot delete pending or processing jobs (requirement 11.4).
	if job.Status == "pending" || job.Status == "processing" {
		return ErrJobConflict
	}

	// Atomically delete Steps then Job (requirement 11.1).
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("job_id = ?", jobID).Delete(&models.Step{}).Error; err != nil {
			s.logger.Error("delete_job: delete steps failed", "job_id", jobID, "error", err)
			return err
		}
		if err := tx.Delete(&job).Error; err != nil {
			s.logger.Error("delete_job: delete job failed", "job_id", jobID, "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		return ErrDBUnavailable
	}

	return nil
}
