package dto

import "time"

// CreateJobRequest holds the payload for the POST /api/jobs/generate endpoint.
type CreateJobRequest struct {
	ModelImageURL   string `json:"modelImageUrl"   validate:"required,url,startswith=https://"`
	ProductImageURL string `json:"productImageUrl" validate:"required,url,startswith=https://"`
	ProductName     string `json:"productName"     validate:"required,max=200"`
	ProductCategory string `json:"productCategory" validate:"required,oneof=beauty fashion electronics health"`
	TargetAudience  string `json:"targetAudience"  validate:"required,oneof=man woman children unisex"`
	Orientation     string `json:"orientation"     validate:"required,oneof=portrait landscape square"`
	Resolution      string `json:"resolution"      validate:"required,oneof=720p 1080p 4k"`
}

// CreateJobResponse is returned on successful job submission (HTTP 202).
type CreateJobResponse struct {
	JobID string `json:"jobId"`
}

// JobResponse is returned by GET /api/jobs/:id with full step details.
type JobResponse struct {
	ID        string         `json:"id"`
	Status    string         `json:"status"`
	Progress  int            `json:"progress"`
	Steps     []StepResponse `json:"steps"`
	CreatedAt time.Time      `json:"createdAt"`
}

// StepResponse represents a single pipeline step within a JobResponse.
type StepResponse struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	VideoURL *string `json:"videoUrl"`
}

// JobListResponse is returned by GET /api/jobs with pagination metadata.
type JobListResponse struct {
	Jobs  []JobSummary `json:"jobs"`
	Total int64        `json:"total,omitempty"`
	Page  int          `json:"page,omitempty"`
	Limit int          `json:"limit,omitempty"`
}

// JobSummary is a lightweight job representation used in list responses.
type JobSummary struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	ThumbnailURL *string   `json:"thumbnailUrl"`
}

// PaginationParams carries page and limit values parsed from query parameters.
type PaginationParams struct {
	Page  int
	Limit int
}
