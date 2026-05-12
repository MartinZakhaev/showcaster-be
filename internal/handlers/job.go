package handlers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"showcaster-be/internal/dto"
	"showcaster-be/internal/middleware"
	"showcaster-be/internal/services"
)

// JobHandler handles all job-related HTTP routes.
type JobHandler struct {
	JobService services.JobService
	Validator  *validator.Validate
}

// CreateJob handles POST /api/jobs/generate.
//
//	@Summary		Submit a video generation job
//	@Description	Atomically creates one Job record (status=pending) and four Step records
//	@Description	(Hook, Problem, Solution, Closure — each status=pending) in a single DB transaction,
//	@Description	then enqueues the job for async processing by the background worker.
//	@Description
//	@Description	**Accepted enum values:**
//	@Description	- `productCategory`: beauty, fashion, electronics, health
//	@Description	- `targetAudience`: man, woman, children, unisex
//	@Description	- `orientation`: portrait, landscape, square
//	@Description	- `resolution`: 720p, 1080p, 4k
//	@Description
//	@Description	Both image URLs must be valid HTTPS URLs (must start with `https://`).
//	@Tags			Jobs
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		dto.CreateJobRequest			true	"Job payload"
//	@Success		202		{object}	dto.CreateJobDocResponse		"Job accepted — poll GET /api/jobs/{jobId} for status"
//	@Failure		400		{object}	dto.ValidationErrorResponse		"Missing or invalid field"
//	@Failure		401		{object}	dto.ErrorResponse				"Missing or invalid JWT"
//	@Failure		503		{object}	dto.ErrorResponse				"Job queue full or database unavailable"
//	@Router			/api/jobs/generate [post]
func (h *JobHandler) CreateJob(c *fiber.Ctx) error {
	var req dto.CreateJobRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			field := ve[0]
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("validation failed on field: "+field.Field(), field.Field()),
			)
		}
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	userID := middleware.ExtractUserID(c)

	resp, err := h.JobService.CreateJob(c.Context(), userID, req)
	if err != nil {
		if errors.Is(err, services.ErrQueueFull) || errors.Is(err, services.ErrDBUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail(err.Error()))
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
	}

	return c.Status(fiber.StatusAccepted).JSON(dto.OK(fiber.Map{"jobId": resp.JobID}))
}

// GetJob handles GET /api/jobs/:id.
//
//	@Summary		Get job status
//	@Description	Returns the full status of a job including all 4 pipeline steps and their output video URLs.
//	@Description
//	@Description	**Job status values:** pending → processing → completed | failed
//	@Description	**Step status values:** pending → processing → completed | failed
//	@Description	**Progress:** integer 0–100, calculated as (completed steps / 4) × 100
//	@Description
//	@Description	`videoUrl` is `null` for steps that are still pending or processing.
//	@Description	It becomes a Cloudinary HTTPS URL once the step is completed.
//	@Tags			Jobs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Job ID (UUID v4)"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Success		200	{object}	dto.GetJobResponse		"Job details with step statuses"
//	@Failure		401	{object}	dto.ErrorResponse		"Missing or invalid JWT"
//	@Failure		403	{object}	dto.ErrorResponse		"Job belongs to a different user"
//	@Failure		404	{object}	dto.ErrorResponse		"Job not found"
//	@Failure		503	{object}	dto.ErrorResponse		"Database unavailable"
//	@Router			/api/jobs/{id} [get]
func (h *JobHandler) GetJob(c *fiber.Ctx) error {
	userID := middleware.ExtractUserID(c)
	jobID := c.Params("id")

	resp, err := h.JobService.GetJob(c.Context(), userID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrJobNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.Fail(err.Error()))
		case errors.Is(err, services.ErrJobForbidden):
			return c.Status(fiber.StatusForbidden).JSON(dto.Fail(err.Error()))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail(err.Error()))
		default:
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		}
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(resp))
}

// ListJobs handles GET /api/jobs.
//
//	@Summary		List jobs
//	@Description	Returns a paginated list of the authenticated user's jobs ordered by `createdAt` descending.
//	@Description	Only jobs belonging to the authenticated user are returned.
//	@Description
//	@Description	**Pagination defaults:** page=1, limit=100
//	@Description	**Limit range:** 0–100. When limit=0 an empty array is returned alongside the total count.
//	@Tags			Jobs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page	query		int						false	"Page number (min 1, default 1)"			minimum(1)
//	@Param			limit	query		int						false	"Items per page (0–100, default 100)"		minimum(0)	maximum(100)
//	@Success		200		{object}	dto.ListJobsResponse	"Paginated job list with metadata"
//	@Failure		400		{object}	dto.ErrorResponse		"Invalid page or limit value"
//	@Failure		401		{object}	dto.ErrorResponse		"Missing or invalid JWT"
//	@Failure		503		{object}	dto.ErrorResponse		"Database unavailable"
//	@Router			/api/jobs [get]
func (h *JobHandler) ListJobs(c *fiber.Ctx) error {
	userID := middleware.ExtractUserID(c)

	pageStr := c.Query("page")
	limitStr := c.Query("limit")

	page := c.QueryInt("page", 0)
	limit := c.QueryInt("limit", 0)

	if pageStr != "" {
		if page < 1 {
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("page must be a positive integer (minimum 1)", "page"),
			)
		}
	}

	if limitStr != "" {
		if (limit == 0 && limitStr != "0") || limit < 0 || limit > 100 {
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("limit must be an integer between 0 and 100", "limit"),
			)
		}
	}

	params := dto.PaginationParams{Page: page, Limit: limit}

	resp, err := h.JobService.ListJobs(c.Context(), userID, params)
	if err != nil {
		if errors.Is(err, services.ErrDBUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail(err.Error()))
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(resp))
}

// DeleteJob handles DELETE /api/jobs/:id.
//
//	@Summary		Delete a job
//	@Description	Permanently deletes a job and all its associated steps in a single atomic transaction.
//	@Description
//	@Description	**Constraints:**
//	@Description	- Only the job owner can delete it.
//	@Description	- Only jobs with status `completed` or `failed` can be deleted.
//	@Description	- Attempting to delete a `pending` or `processing` job returns 409.
//	@Tags			Jobs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Job ID (UUID v4)"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Success		200	{object}	dto.DeleteJobResponse	"Job and all steps deleted"
//	@Failure		401	{object}	dto.ErrorResponse		"Missing or invalid JWT"
//	@Failure		403	{object}	dto.ErrorResponse		"Job belongs to a different user"
//	@Failure		404	{object}	dto.ErrorResponse		"Job not found"
//	@Failure		409	{object}	dto.ErrorResponse		"Job is still pending or processing — cannot delete"
//	@Failure		503	{object}	dto.ErrorResponse		"Database unavailable"
//	@Router			/api/jobs/{id} [delete]
func (h *JobHandler) DeleteJob(c *fiber.Ctx) error {
	userID := middleware.ExtractUserID(c)
	jobID := c.Params("id")

	err := h.JobService.DeleteJob(c.Context(), userID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrJobNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.Fail(err.Error()))
		case errors.Is(err, services.ErrJobForbidden):
			return c.Status(fiber.StatusForbidden).JSON(dto.Fail(err.Error()))
		case errors.Is(err, services.ErrJobConflict):
			return c.Status(fiber.StatusConflict).JSON(dto.Fail(err.Error()))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail(err.Error()))
		default:
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		}
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(fiber.Map{"message": "job deleted successfully"}))
}
