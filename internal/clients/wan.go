package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"showcaster-be/internal/models"
)

// Sentinel errors. Same shape as Veo so the worker code is identical.
var (
	ErrWanGenerationFailed = errors.New("wan: video generation failed")
	ErrWanTimeout          = errors.New("wan: video generation timed out")
)

const (
	// Singapore endpoint. Use the Beijing endpoint if your API key is from CN region.
	wanBaseURL       = "https://dashscope-intl.aliyuncs.com/api/v1"
	wanModel         = "wan2.7-i2v-2026-04-25"
	wanPollInterval  = 15 * time.Second
	wanTimeout       = 15 * time.Minute
	wanVideoDuration = 8 // seconds
)

// WanClient implements the ReplicateClient interface using Alibaba Cloud
// Model Studio (DashScope) Wan 2.7 image-to-video API.
type WanClient struct {
	apiKey     string
	httpClient *http.Client
	prompter   *OpenAIClient // uses DashScope deepseek-v4-flash by default
	logger     *slog.Logger
}

// NewWanClient creates a WanClient with the given DashScope API key.
// Prompt generation always uses deepseek-v4-flash via the DashScope
// OpenAI-compatible endpoint (same API key). Pass a non-nil overridePrompter
// to substitute a different prompt client (e.g. for testing).
func NewWanClient(apiKey string, overridePrompter *OpenAIClient) *WanClient {
	prompter := overridePrompter
	if prompter == nil {
		prompter = NewDashScopePromptClient(apiKey)
	}
	return &WanClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		prompter:   prompter,
		logger:     slog.Default(),
	}
}

// ── Request / response types ──────────────────────────────────────────────────

type wanMediaAsset struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type wanInput struct {
	Prompt         string          `json:"prompt"`
	NegativePrompt string          `json:"negative_prompt,omitempty"`
	Media          []wanMediaAsset `json:"media"`
}

type wanParameters struct {
	Resolution    string `json:"resolution"`              // "720P" or "1080P"
	Duration      int    `json:"duration"`                // 2..15 seconds
	PromptExtend  bool   `json:"prompt_extend"`
	Watermark     bool   `json:"watermark"`
}

type wanCreateRequest struct {
	Model      string        `json:"model"`
	Input      wanInput      `json:"input"`
	Parameters wanParameters `json:"parameters"`
}

type wanOutput struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
	VideoURL   string `json:"video_url,omitempty"`
	OrigPrompt string `json:"orig_prompt,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type wanResponse struct {
	Output    wanOutput `json:"output"`
	RequestID string    `json:"request_id"`
}

// ── Core ──────────────────────────────────────────────────────────────────────

// doRequest is a shared helper for HTTP calls with the DashScope Bearer token.
func (c *WanClient) doRequest(ctx context.Context, method, url string, body interface{}, extraHeaders map[string]string) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("wan: marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("wan: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("wan: http: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("wan: read body: %w", err)
	}
	return rawBody, resp.StatusCode, nil
}

// wanErrorResponse is used to detect structured API errors (e.g. InvalidApiKey)
// returned in the top-level body of a non-2xx response.
type wanErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// submitTask creates an async video generation task and returns the task_id.
func (c *WanClient) submitTask(ctx context.Context, job models.Job, stepName string) (string, error) {
	// 4a. Guard: ModelImageURL must be non-empty (Req 4.7)
	if job.ModelImageURL == "" {
		return "", fmt.Errorf("wan: job.ModelImageURL is empty — cannot submit task")
	}

	// 1. Build the prompt — use deepseek-v4-flash via DashScope (or override prompter)
	var prompt string
	if c.prompter != nil {
		scene, err := c.prompter.GenerateScenePrompt(ctx, job, stepName)
		if err != nil {
			c.logger.Warn("wan: prompt generation failed, falling back to template", "step", stepName, "error", err)
			prompt = buildVeoPrompt(stepName, job)
		} else {
			c.logger.Info("wan: using generated prompt", "step", stepName, "sceneId", scene.SceneID)
			prompt = scene.VisualPrompt
		}
	} else {
		prompt = buildVeoPrompt(stepName, job)
	}

	// 4g. Use the resolveResolution method (Req 6.3, 6.4)
	resolution := c.resolveResolution(job.Resolution)

	// 4f. Build media slice; append driving_audio if present (Req 4.6)
	media := []wanMediaAsset{
		{Type: "first_frame", URL: job.ModelImageURL},
	}
	if job.DrivingAudioURL != "" {
		media = append(media, wanMediaAsset{Type: "driving_audio", URL: job.DrivingAudioURL})
	}

	reqBody := wanCreateRequest{
		Model: wanModel,
		Input: wanInput{
			Prompt:         prompt,
			NegativePrompt: "low resolution, blurry, distorted, deformed, extra fingers, bad proportions, watermark, text, logo, typography",
			Media:          media,
		},
		Parameters: wanParameters{
			Resolution:   resolution,
			Duration:     clampDuration(wanVideoDuration, c.logger), // 4c. clamp duration (Req 7.3, 7.4)
			PromptExtend: true,
			Watermark:    false,
		},
	}

	endpoint := wanBaseURL + "/services/aigc/video-generation/video-synthesis"
	c.logger.Info("wan: submitting task", "step", stepName, "endpoint", endpoint, "model", wanModel, "resolution", resolution)

	rawBody, status, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody, map[string]string{
		"X-DashScope-Async": "enable",
	})
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		// 4d. InvalidApiKey detection (Req 14.1)
		var apiErr wanErrorResponse
		if json.Unmarshal(rawBody, &apiErr) == nil && apiErr.Code == "InvalidApiKey" {
			return "", fmt.Errorf("wan: invalid DashScope API key (request_id=%s): %s", apiErr.RequestID, apiErr.Message)
		}
		return "", fmt.Errorf("wan: submit task: status %d: %s", status, string(rawBody))
	}

	var resp wanResponse
	if err := json.Unmarshal(rawBody, &resp); err != nil {
		return "", fmt.Errorf("wan: decode submit response (body: %s): %w", string(rawBody), err)
	}
	if resp.Output.TaskID == "" {
		return "", fmt.Errorf("wan: submit task: empty task_id (status=%s, message=%s)", resp.Output.TaskStatus, resp.Output.Message)
	}

	c.logger.Info("wan: task submitted", "step", stepName, "taskId", resp.Output.TaskID)
	return resp.Output.TaskID, nil
}

// pollTask queries the status of a running task.
func (c *WanClient) pollTask(ctx context.Context, taskID string) (*wanOutput, error) {
	endpoint := fmt.Sprintf("%s/tasks/%s", wanBaseURL, taskID)
	rawBody, status, err := c.doRequest(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("wan: poll task: status %d: %s", status, string(rawBody))
	}

	var resp wanResponse
	if err := json.Unmarshal(rawBody, &resp); err != nil {
		return nil, fmt.Errorf("wan: decode poll response (body: %s): %w", string(rawBody), err)
	}
	return &resp.Output, nil
}

// Generate implements the ReplicateClient interface using Wan 2.7.
// Returns the generated video URL (an Alibaba OSS URL valid for 24 hours).
// The worker will pass this URL to Cloudinary.UploadVideoFromURL for permanent storage.
func (c *WanClient) Generate(ctx context.Context, job models.Job, stepName string) (string, error) {
	c.logger.Info("wan: starting generation", "jobId", job.ID, "step", stepName)
	start := time.Now()

	taskID, err := c.submitTask(ctx, job, stepName)
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(wanTimeout)
	pollCount := 0

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wanPollInterval):
		}

		pollCount++
		if time.Now().After(deadline) {
			c.logger.Error("wan: generation timed out", "jobId", job.ID, "step", stepName, "elapsed", time.Since(start))
			return "", ErrWanTimeout
		}

		c.logger.Info("wan: polling task", "jobId", job.ID, "step", stepName, "poll", pollCount, "elapsed", time.Since(start).Round(time.Second))

		out, err := c.pollTask(ctx, taskID)
		if err != nil {
			c.logger.Warn("wan: poll error (will retry)", "jobId", job.ID, "step", stepName, "error", err)
			continue
		}

		c.logger.Info("wan: poll result", "jobId", job.ID, "step", stepName, "status", out.TaskStatus)

		switch strings.ToUpper(out.TaskStatus) {
		case "SUCCEEDED":
			if out.VideoURL == "" {
				return "", fmt.Errorf("wan: task succeeded but video_url is empty")
			}
			c.logger.Info("wan: generation complete", "jobId", job.ID, "step", stepName, "elapsed", time.Since(start).Round(time.Second), "videoUrl", out.VideoURL)
			return out.VideoURL, nil

		case "FAILED", "CANCELED":
			c.logger.Error("wan: task terminal failure", "jobId", job.ID, "step", stepName, "code", out.Code, "message", out.Message)
			return "", fmt.Errorf("%w: %s — %s", ErrWanGenerationFailed, out.Code, out.Message)

		case "UNKNOWN":
			// 4e. Include taskID in the error message (Req 13.1)
			return "", fmt.Errorf("wan: task %s status UNKNOWN — task may have expired or does not exist", taskID)

		case "PENDING", "RUNNING":
			// keep polling
			continue

		default:
			c.logger.Warn("wan: unrecognized task status", "status", out.TaskStatus)
			continue
		}
	}
}

// resolveResolution maps our internal resolution strings to Wan's accepted values.
// Wan only supports 720P and 1080P. 4K falls back to 1080P with a warning.
// Unrecognized values fall back to 720P with a warning. (Req 6.3, 6.4)
func (c *WanClient) resolveResolution(internal string) string {
	switch internal {
	case "720p":
		return "720P"
	case "1080p":
		return "1080P"
	case "4k":
		c.logger.Warn("wan: 4k resolution not supported, substituting 1080P", "original", internal, "substituted", "1080P")
		return "1080P"
	default:
		c.logger.Warn("wan: unrecognized resolution, defaulting to 720P", "original", internal, "defaulted", "720P")
		return "720P"
	}
}

// clampDuration clamps d to the inclusive range [2, 15] accepted by DashScope.
// A warning is logged when clamping occurs. (Req 7.3, 7.4)
func clampDuration(d int, logger *slog.Logger) int {
	if d < 2 {
		logger.Warn("wan: duration below minimum, clamping to 2", "configured", d)
		return 2
	}
	if d > 15 {
		logger.Warn("wan: duration above maximum, clamping to 15", "configured", d)
		return 15
	}
	return d
}
