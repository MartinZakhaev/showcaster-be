package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"showcaster-be/internal/models"
)

// Sentinel errors returned by ReplicateClient.Generate.
var (
	// ErrGenerationFailed is returned when the Replicate prediction reaches the
	// "failed" terminal state.
	ErrGenerationFailed = errors.New("replicate: prediction failed")

	// ErrTimeout is returned when the Replicate prediction does not reach a
	// terminal state within 10 minutes. A cancellation request is sent before
	// this error is returned.
	ErrTimeout = errors.New("replicate: prediction timed out")
)

// stepPrompts maps a Step name to its text prompt template.
// {productName} and {targetAudience} are replaced at runtime.
var stepPrompts = map[string]string{
	"Hook":     "Attention-grabbing opening scene showcasing {productName} for {targetAudience}",
	"Problem":  "Scene depicting the problem that {productName} solves for {targetAudience}",
	"Solution": "Scene showing {productName} as the solution, highlighting key benefits",
	"Closure":  "Compelling call-to-action closing scene for {productName}",
}

const (
	replicateBaseURL  = "https://api.replicate.com/v1"
	replicateModel    = "wan-ai/wan2.1-i2v-480p"
	pollInterval      = 5 * time.Second
	generationTimeout = 10 * time.Minute
)

// ReplicateClient defines the interface for generating video clips via the
// Replicate API.
type ReplicateClient interface {
	// Generate submits a prediction for the given job and step, polls until a
	// terminal state is reached, and returns the output video URL on success.
	// It returns ErrGenerationFailed when the prediction fails, or ErrTimeout
	// when no terminal state is reached within 10 minutes.
	Generate(ctx context.Context, job models.Job, stepName string) (videoURL string, err error)
}

// ReplicateClientImpl is the production implementation of ReplicateClient.
type ReplicateClientImpl struct {
	apiToken   string
	httpClient *http.Client
}

// NewReplicateClient returns a ready-to-use *ReplicateClientImpl that
// authenticates all requests with apiToken.
func NewReplicateClient(apiToken string) *ReplicateClientImpl {
	return &ReplicateClientImpl{
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// buildPrompt returns the text prompt for the given step, with {productName}
// and {targetAudience} substituted from the job.
func buildPrompt(stepName string, job models.Job) string {
	template, ok := stepPrompts[stepName]
	if !ok {
		// Fallback for unknown step names — should not happen in practice.
		template = "Scene for {productName} targeting {targetAudience}"
	}
	prompt := strings.ReplaceAll(template, "{productName}", job.ProductName)
	prompt = strings.ReplaceAll(prompt, "{targetAudience}", job.TargetAudience)
	return prompt
}

// predictionRequest is the JSON body sent to POST /v1/predictions.
type predictionRequest struct {
	Version string                 `json:"version,omitempty"`
	Model   string                 `json:"model"`
	Input   map[string]interface{} `json:"input"`
}

// predictionResponse is the JSON body returned by the Replicate predictions
// endpoints.
type predictionResponse struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Output []string `json:"output"`
	Error  string   `json:"error"`
}

// doRequest performs an HTTP request with the Replicate Bearer token attached
// and decodes the JSON response into dest.
func (c *ReplicateClientImpl) doRequest(method, url string, body interface{}, dest interface{}) (*http.Response, error) {
	var reqBody *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("replicate: marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("replicate: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replicate: http: %w", err)
	}
	defer resp.Body.Close()

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return resp, fmt.Errorf("replicate: decode response: %w", err)
		}
	}
	return resp, nil
}

// submitPrediction POSTs a new prediction to the Replicate API and returns the
// prediction ID.
func (c *ReplicateClientImpl) submitPrediction(job models.Job, stepName string) (string, error) {
	prompt := buildPrompt(stepName, job)

	reqBody := predictionRequest{
		Model: replicateModel,
		Input: map[string]interface{}{
			"image":            job.ModelImageURL,
			"product_image":    job.ProductImageURL,
			"product_name":     job.ProductName,
			"product_category": job.ProductCategory,
			"target_audience":  job.TargetAudience,
			"orientation":      job.Orientation,
			"resolution":       job.Resolution,
			"prompt":           prompt,
		},
	}

	var pred predictionResponse
	resp, err := c.doRequest(http.MethodPost, replicateBaseURL+"/predictions", reqBody, &pred)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("replicate: submit prediction: unexpected status %d", resp.StatusCode)
	}
	if pred.ID == "" {
		return "", fmt.Errorf("replicate: submit prediction: empty prediction ID in response")
	}
	return pred.ID, nil
}

// pollPrediction polls GET /v1/predictions/{id} and returns the prediction
// response once a terminal state is reached, or an error.
func (c *ReplicateClientImpl) pollPrediction(predictionID string) (*predictionResponse, error) {
	url := fmt.Sprintf("%s/predictions/%s", replicateBaseURL, predictionID)
	var pred predictionResponse
	resp, err := c.doRequest(http.MethodGet, url, nil, &pred)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("replicate: poll prediction: unexpected status %d", resp.StatusCode)
	}
	return &pred, nil
}

// cancelPrediction sends a POST to /v1/predictions/{id}/cancel.
func (c *ReplicateClientImpl) cancelPrediction(predictionID string) error {
	url := fmt.Sprintf("%s/predictions/%s/cancel", replicateBaseURL, predictionID)
	resp, err := c.doRequest(http.MethodPost, url, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("replicate: cancel prediction: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// Generate implements ReplicateClient. It submits a prediction for the given
// job and step, polls every 5 seconds, and returns the output video URL on
// success. It returns ErrGenerationFailed when the prediction fails, or
// ErrTimeout (after sending a cancellation request) when no terminal state is
// reached within 10 minutes.
func (c *ReplicateClientImpl) Generate(ctx context.Context, job models.Job, stepName string) (string, error) {
	predictionID, err := c.submitPrediction(job, stepName)
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(generationTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled — attempt to cancel the prediction and propagate.
			_ = c.cancelPrediction(predictionID)
			return "", ctx.Err()

		case t := <-ticker.C:
			if t.After(deadline) {
				// 10-minute timeout exceeded.
				_ = c.cancelPrediction(predictionID)
				return "", ErrTimeout
			}

			pred, err := c.pollPrediction(predictionID)
			if err != nil {
				// Transient poll error — keep trying until deadline.
				continue
			}

			switch pred.Status {
			case "succeeded":
				if len(pred.Output) == 0 {
					return "", fmt.Errorf("replicate: prediction succeeded but output is empty")
				}
				return pred.Output[0], nil

			case "failed":
				return "", ErrGenerationFailed

			default:
				// starting, processing, or any other non-terminal state — keep polling.
			}
		}
	}
}
