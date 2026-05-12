package dto

import "time"

// ---------------------------------------------------------------------------
// Typed response wrappers used exclusively for swaggo documentation.
// Each type represents the concrete shape of a specific endpoint's response
// so that Scalar / Swagger UI can render accurate examples.
// ---------------------------------------------------------------------------

// ── Auth ────────────────────────────────────────────────────────────────────

// RegisterResponseData is the data payload returned on successful registration.
type RegisterResponseData struct {
	Message string `json:"message" example:"registration successful; please verify your email with the OTP sent"`
}

// RegisterResponse is the full envelope returned by POST /api/auth/register (201).
type RegisterResponse struct {
	Success bool                 `json:"success" example:"true"`
	Data    RegisterResponseData `json:"data"`
}

// LoginResponseData is the data payload returned on successful login.
type LoginResponseData struct {
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI1NWYwZjBhNy0xMjM0LTQ1NjctODkwYS1iY2RlZjAxMjM0NTYiLCJpYXQiOjE3MTYwMDAwMDAsImV4cCI6MTcxNjA4NjQwMH0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"`
}

// LoginResponse is the full envelope returned by POST /api/auth/login (200).
type LoginResponse struct {
	Success bool              `json:"success" example:"true"`
	Data    LoginResponseData `json:"data"`
}

// VerifyOTPResponse is the full envelope returned by POST /api/auth/verify-otp (200).
// The data shape is identical to LoginResponse.
type VerifyOTPResponse = LoginResponse

// ResendOTPResponseData is the data payload returned on successful OTP resend.
type ResendOTPResponseData struct {
	Message string `json:"message" example:"OTP resent successfully"`
}

// ResendOTPResponse is the full envelope returned by POST /api/auth/resend-otp (200).
type ResendOTPResponse struct {
	Success bool                  `json:"success" example:"true"`
	Data    ResendOTPResponseData `json:"data"`
}

// ── Upload ──────────────────────────────────────────────────────────────────

// UploadImageResponseData is the data payload returned on successful image upload.
type UploadImageResponseData struct {
	URL string `json:"url" example:"https://res.cloudinary.com/demo/image/upload/v1716000000/showcaster/images/abc123.jpg"`
}

// UploadImageResponse is the full envelope returned by POST /api/upload/image (200).
type UploadImageResponse struct {
	Success bool                    `json:"success" example:"true"`
	Data    UploadImageResponseData `json:"data"`
}

// ── Jobs ────────────────────────────────────────────────────────────────────

// CreateJobResponseData is the data payload returned on successful job submission.
type CreateJobResponseData struct {
	JobID string `json:"jobId" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// CreateJobDocResponse is the full envelope returned by POST /api/jobs/generate (202).
type CreateJobDocResponse struct {
	Success bool                  `json:"success" example:"true"`
	Data    CreateJobResponseData `json:"data"`
}

// StepResponseDoc is the documented shape of a single pipeline step.
type StepResponseDoc struct {
	Name     string  `json:"name"     example:"Hook"`
	Status   string  `json:"status"   example:"completed"`
	VideoURL *string `json:"videoUrl" example:"https://res.cloudinary.com/demo/video/upload/v1716000000/showcaster/videos/hook.mp4"`
}

// GetJobResponseData is the data payload returned by GET /api/jobs/:id.
type GetJobResponseData struct {
	ID        string            `json:"id"        example:"550e8400-e29b-41d4-a716-446655440000"`
	Status    string            `json:"status"    example:"processing"`
	Progress  int               `json:"progress"  example:"25"`
	Steps     []StepResponseDoc `json:"steps"`
	CreatedAt time.Time         `json:"createdAt" example:"2024-05-18T10:00:00Z"`
}

// GetJobResponse is the full envelope returned by GET /api/jobs/:id (200).
type GetJobResponse struct {
	Success bool               `json:"success" example:"true"`
	Data    GetJobResponseData `json:"data"`
}

// JobSummaryDoc is the documented shape of a job in the list response.
type JobSummaryDoc struct {
	ID           string    `json:"id"           example:"550e8400-e29b-41d4-a716-446655440000"`
	Status       string    `json:"status"       example:"completed"`
	CreatedAt    time.Time `json:"createdAt"    example:"2024-05-18T10:00:00Z"`
	ThumbnailURL *string   `json:"thumbnailUrl" example:"https://res.cloudinary.com/demo/image/upload/v1716000000/showcaster/images/thumb.jpg"`
}

// ListJobsResponseData is the data payload returned by GET /api/jobs.
type ListJobsResponseData struct {
	Jobs  []JobSummaryDoc `json:"jobs"`
	Total int64           `json:"total" example:"42"`
	Page  int             `json:"page"  example:"1"`
	Limit int             `json:"limit" example:"10"`
}

// ListJobsResponse is the full envelope returned by GET /api/jobs (200).
type ListJobsResponse struct {
	Success bool                 `json:"success" example:"true"`
	Data    ListJobsResponseData `json:"data"`
}

// DeleteJobResponseData is the data payload returned on successful job deletion.
type DeleteJobResponseData struct {
	Message string `json:"message" example:"job deleted successfully"`
}

// DeleteJobResponse is the full envelope returned by DELETE /api/jobs/:id (200).
type DeleteJobResponse struct {
	Success bool                  `json:"success" example:"true"`
	Data    DeleteJobResponseData `json:"data"`
}

// ── Health ──────────────────────────────────────────────────────────────────

// HealthResponseData is the data payload returned by GET /health.
type HealthResponseData struct {
	Status string `json:"status" example:"ok"`
}

// HealthResponse is the full envelope returned by GET /health (200).
type HealthResponse struct {
	Success bool               `json:"success" example:"true"`
	Data    HealthResponseData `json:"data"`
}

// ── Error responses ─────────────────────────────────────────────────────────

// ErrorResponse is the standard error envelope returned on all failures.
type ErrorResponse struct {
	Success bool   `json:"success" example:"false"`
	Error   string `json:"error"   example:"human-readable error message"`
}

// ValidationErrorResponse is the error envelope returned on validation failures.
// It includes the name of the offending field.
type ValidationErrorResponse struct {
	Success bool   `json:"success" example:"false"`
	Error   string `json:"error"   example:"validation failed: Email email"`
	Field   string `json:"field"   example:"Email"`
}
