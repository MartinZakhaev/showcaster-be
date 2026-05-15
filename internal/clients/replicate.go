package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// stepPrompts is no longer used — prompts are now built as structured JSON
// by buildPrompt. Kept as documentation of the original scene intent.
//
// Hook:     Attention-grabbing opening scene
// Problem:  Relatable problem scene
// Solution: Product-as-solution scene
// Closure:  Call-to-action closing scene

const (
	replicateBaseURL  = "https://api.replicate.com/v1"
	replicateModel    = "wavespeedai/wan-2.1-i2v-480p"
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
// buildPrompt returns a structured JSON prompt for the given step so the model
// receives consistent, machine-readable context about the product and scene.
func buildPrompt(stepName string, job models.Job) string {
	sceneDescriptions := map[string]string{
		"Hook":     "Attention-grabbing opening scene that immediately showcases the product",
		"Problem":  "Relatable scene depicting the everyday problem the product solves",
		"Solution": "Satisfying scene showing the product as the perfect solution with clear benefits",
		"Closure":  "Compelling call-to-action closing scene with the product prominently featured",
	}
	scene, ok := sceneDescriptions[stepName]
	if !ok {
		scene = "Product showcase scene"
	}

	type promptPayload struct {
		Scene           string `json:"scene"`
		Step            string `json:"step"`
		ProductName     string `json:"product_name"`
		ProductCategory string `json:"product_category"`
		TargetAudience  string `json:"target_audience"`
		Orientation     string `json:"orientation"`
		Resolution      string `json:"resolution"`
		Style           string `json:"style"`
	}

	p := promptPayload{
		Scene:           scene,
		Step:            stepName,
		ProductName:     job.ProductName,
		ProductCategory: job.ProductCategory,
		TargetAudience:  job.TargetAudience,
		Orientation:     job.Orientation,
		Resolution:      job.Resolution,
		Style:           "cinematic, high quality, professional affiliate marketing video",
	}

	data, err := json.Marshal(p)
	if err != nil {
		// Fallback to plain text if marshalling fails
		return fmt.Sprintf("%s: %s for %s (%s)", stepName, job.ProductName, job.TargetAudience, job.ProductCategory)
	}
	return string(data)
}

// predictionRequest is the JSON body sent to POST /v1/models/{owner}/{name}/predictions.
// The model-specific endpoint only needs the input — no version or model field.
type predictionRequest struct {
	Input map[string]interface{} `json:"input"`
}

// predictionResponse is the JSON body returned by the Replicate predictions
// endpoints. Fields use json.RawMessage where the type varies across models
// and error states.
type predictionResponse struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status"` // can be string or number in error responses
	Output json.RawMessage `json:"output"` // can be []string, string, or null
	Error  interface{}     `json:"error"`  // can be string or object
}

// statusString safely extracts the status as a string regardless of whether
// Replicate returned it as a JSON string or number.
func (p *predictionResponse) statusString() string {
	if len(p.Status) == 0 {
		return ""
	}
	// Try string first (normal case: "starting", "processing", "succeeded", "failed")
	var s string
	if err := json.Unmarshal(p.Status, &s); err == nil {
		return s
	}
	// Fallback: number — treat as failed
	return "failed"
}

// outputURLs extracts video URLs from the output field which can be:
//   - []string  (most models)
//   - string    (some models return a single URL)
//   - null / absent
func (p *predictionResponse) outputURLs() []string {
	if len(p.Output) == 0 || string(p.Output) == "null" {
		return nil
	}
	// Try array of strings
	var arr []string
	if err := json.Unmarshal(p.Output, &arr); err == nil {
		return arr
	}
	// Try single string
	var s string
	if err := json.Unmarshal(p.Output, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
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

	// Read the full body so we can include it in error messages.
	var rawBody bytes.Buffer
	rawBody.ReadFrom(resp.Body)

	if dest != nil {
		if err := json.Unmarshal(rawBody.Bytes(), dest); err != nil {
			return resp, fmt.Errorf("replicate: decode response (status %d, body: %s): %w",
				resp.StatusCode, rawBody.String(), err)
		}
	}

	// Attach the raw body to the response for error reporting upstream.
	resp.Body = io.NopCloser(&rawBody)
	return resp, nil
}

// submitPrediction POSTs a new prediction to the model-specific endpoint
// /v1/models/{owner}/{name}/predictions and returns the prediction ID.
func (c *ReplicateClientImpl) submitPrediction(job models.Job, stepName string) (string, error) {
	prompt := buildPrompt(stepName, job)

	// Use the model-specific endpoint — no "model" field needed in the body.
	// See: https://replicate.com/wavespeedai/wan-2.1-i2v-480p/api/api-reference
	endpoint := fmt.Sprintf("%s/models/%s/predictions", replicateBaseURL, replicateModel)

	reqBody := predictionRequest{
		Input: map[string]interface{}{
			"image":  job.ModelImageURL,
			"prompt": prompt,
		},
	}

	var pred predictionResponse
	resp, err := c.doRequest(http.MethodPost, endpoint, reqBody, &pred)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read the body for a useful error message
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return "", fmt.Errorf("replicate: submit prediction: status %d: %s", resp.StatusCode, errBody.String())
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
			_ = c.cancelPrediction(predictionID)
			return "", ctx.Err()

		case t := <-ticker.C:
			if t.After(deadline) {
				_ = c.cancelPrediction(predictionID)
				return "", ErrTimeout
			}

			pred, err := c.pollPrediction(predictionID)
			if err != nil {
				// Transient poll error — keep trying until deadline.
				continue
			}

			switch pred.statusString() {
			case "succeeded":
				urls := pred.outputURLs()
				if len(urls) == 0 {
					return "", fmt.Errorf("replicate: prediction succeeded but output is empty")
				}
				return urls[0], nil

			case "failed":
				return "", ErrGenerationFailed

			default:
				// starting, processing, or any other non-terminal state — keep polling.
			}
		}
	}
}
