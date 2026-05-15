package clients

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"showcaster-be/internal/models"
)

// Sentinel errors — same names as the old Replicate sentinels so the worker
// doesn't need to change.
var (
	ErrVeoGenerationFailed = errors.New("veo: video generation failed")
	ErrVeoTimeout          = errors.New("veo: video generation timed out")
)

const (
	veoModel         = "veo-3.1-generate-preview"
	veoPollInterval  = 10 * time.Second
	veoTimeout       = 15 * time.Minute
	veoVideoDuration = 8
)

// VeoClient implements the ReplicateClient interface using the official
// google.golang.org/genai SDK with a Google AI Studio API key.
type VeoClient struct {
	client   *genai.Client
	openai   *OpenAIClient
	logger   *slog.Logger
}

// NewVeoClient creates a VeoClient authenticated with a Google AI Studio API key.
// If openaiClient is non-nil, it will be used to generate structured scene prompts
// via GPT-4.1-nano before passing the visual_prompt to Veo.
func NewVeoClient(apiKey string, openaiClient *OpenAIClient) (*VeoClient, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("veo: init genai client: %w", err)
	}
	return &VeoClient{
		client: client,
		openai: openaiClient,
		logger: slog.Default(),
	}, nil
}

// fetchImage downloads an image URL and returns the raw bytes plus MIME type.
func fetchImage(ctx context.Context, imageURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("veo: fetch image: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("veo: fetch image: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("veo: read image body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	var mimeType string
	switch {
	case strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg"):
		mimeType = "image/jpeg"
	case strings.Contains(ct, "png"):
		mimeType = "image/png"
	default:
		sniffed := http.DetectContentType(data[:min512(len(data))])
		if strings.Contains(sniffed, "jpeg") {
			mimeType = "image/jpeg"
		} else {
			mimeType = "image/png"
		}
	}

	return data, mimeType, nil
}

func min512(n int) int {
	if n < 512 {
		return n
	}
	return 512
}

// orientationToAspectRatio converts our internal orientation field to Veo's
// aspect ratio strings. Veo only supports 16:9 and 9:16.
func orientationToAspectRatio(orientation string) string {
	switch orientation {
	case "portrait":
		return "9:16"
	default:
		return "16:9"
	}
}

// Generate implements the ReplicateClient interface using Veo via the
// official Google GenAI SDK. It downloads the model image, kicks off a video
// generation, polls for completion, and returns a data URL of the resulting
// MP4 bytes (which the worker uploads to Cloudinary).
func (c *VeoClient) Generate(ctx context.Context, job models.Job, stepName string) (string, error) {
	c.logger.Info("veo: starting generation", "jobId", job.ID, "step", stepName)
	start := time.Now()

	// 1. Fetch the model image
	c.logger.Info("veo: fetching image", "step", stepName, "url", job.ModelImageURL)
	imgBytes, mimeType, err := fetchImage(ctx, job.ModelImageURL)
	if err != nil {
		return "", err
	}
	c.logger.Info("veo: image fetched", "step", stepName, "mimeType", mimeType, "size", len(imgBytes))

	// 2. Build the prompt — use GPT-4.1-nano if available, otherwise fall back
	var veoPrompt string
	if c.openai != nil {
		scene, err := c.openai.GenerateScenePrompt(ctx, job, stepName)
		if err != nil {
			c.logger.Warn("veo: GPT prompt generation failed, falling back to template", "step", stepName, "error", err)
			veoPrompt = buildVeoPrompt(stepName, job)
		} else {
			c.logger.Info("veo: using GPT-generated prompt", "step", stepName, "sceneId", scene.SceneID)
			veoPrompt = scene.VisualPrompt
		}
	} else {
		veoPrompt = buildVeoPrompt(stepName, job)
	}
	aspectRatio := orientationToAspectRatio(job.Orientation)

	duration := int32(veoVideoDuration)
	count := int32(1)

	config := &genai.GenerateVideosConfig{
		AspectRatio:     aspectRatio,
		DurationSeconds: &duration,
		NumberOfVideos:  count,
	}

	image := &genai.Image{
		ImageBytes: imgBytes,
		MIMEType:   mimeType,
	}

	// 3. Submit the generation request
	c.logger.Info("veo: submitting prediction", "step", stepName, "model", veoModel, "aspectRatio", aspectRatio)
	op, err := c.client.Models.GenerateVideos(ctx, veoModel, veoPrompt, image, config)
	if err != nil {
		return "", fmt.Errorf("veo: submit prediction: %w", err)
	}
	c.logger.Info("veo: prediction submitted", "step", stepName, "operation", op.Name)

	// 4. Poll until done or timeout
	deadline := time.Now().Add(veoTimeout)
	pollCount := 0

	for !op.Done {
		pollCount++
		if time.Now().After(deadline) {
			c.logger.Error("veo: generation timed out", "jobId", job.ID, "step", stepName, "elapsed", time.Since(start))
			return "", ErrVeoTimeout
		}

		c.logger.Info("veo: polling operation", "jobId", job.ID, "step", stepName, "poll", pollCount, "elapsed", time.Since(start).Round(time.Second))

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(veoPollInterval):
		}

		op, err = c.client.Operations.GetVideosOperation(ctx, op, nil)
		if err != nil {
			c.logger.Warn("veo: poll error (will retry)", "jobId", job.ID, "step", stepName, "error", err)
			continue
		}
	}

	if op.Error != nil {
		c.logger.Error("veo: generation failed", "jobId", job.ID, "step", stepName, "error", op.Error)
		return "", fmt.Errorf("%w: %v", ErrVeoGenerationFailed, op.Error)
	}

	if op.Response == nil || len(op.Response.GeneratedVideos) == 0 {
		return "", fmt.Errorf("veo: generation succeeded but no videos in response")
	}

	video := op.Response.GeneratedVideos[0].Video
	if video == nil {
		return "", fmt.Errorf("veo: generation succeeded but video is nil")
	}

	// 5. Download the video bytes (the SDK requires an explicit Files.Download call
	//    to populate VideoBytes when the API returned a URI).
	if len(video.VideoBytes) == 0 && video.URI != "" {
		c.logger.Info("veo: downloading video bytes", "jobId", job.ID, "step", stepName, "uri", video.URI)
		if _, err := c.client.Files.Download(ctx, video, nil); err != nil {
			return "", fmt.Errorf("veo: download video: %w", err)
		}
	}

	if len(video.VideoBytes) == 0 {
		return "", fmt.Errorf("veo: generation succeeded but video bytes are empty")
	}

	mt := video.MIMEType
	if mt == "" {
		mt = "video/mp4"
	}
	c.logger.Info("veo: generation complete", "jobId", job.ID, "step", stepName, "elapsed", time.Since(start).Round(time.Second), "mimeType", mt, "videoBytes", len(video.VideoBytes))

	// Return as data URL — Cloudinary's SDK accepts this directly.
	return fmt.Sprintf("data:%s;base64,%s", mt, base64.StdEncoding.EncodeToString(video.VideoBytes)), nil
}

// buildVeoPrompt builds a rich natural-language prompt for Veo.
func buildVeoPrompt(stepName string, job models.Job) string {
	sceneMap := map[string]string{
		"Hook": fmt.Sprintf(
			"Cinematic attention-grabbing opening scene. A %s person discovers and is immediately captivated by %s (%s). "+
				"Dynamic camera movement, vibrant lighting, high energy. Professional affiliate marketing style.",
			job.TargetAudience, job.ProductName, job.ProductCategory),
		"Problem": fmt.Sprintf(
			"Relatable everyday scene showing a %s person struggling with a common problem that %s (%s) solves. "+
				"Authentic, emotional, empathetic tone. Cinematic quality.",
			job.TargetAudience, job.ProductName, job.ProductCategory),
		"Solution": fmt.Sprintf(
			"Satisfying product reveal. A %s person uses %s (%s) and experiences an immediate positive transformation. "+
				"Clean, aspirational, benefit-focused. Professional commercial style.",
			job.TargetAudience, job.ProductName, job.ProductCategory),
		"Closure": fmt.Sprintf(
			"Compelling call-to-action closing scene featuring %s (%s). Confident, persuasive, memorable. "+
				"The product is prominently displayed. %s target audience. Cinematic fade-out.",
			job.ProductName, job.ProductCategory, job.TargetAudience),
	}

	prompt, ok := sceneMap[stepName]
	if !ok {
		prompt = fmt.Sprintf("Professional product showcase for %s targeting %s. Cinematic quality.", job.ProductName, job.TargetAudience)
	}
	return prompt
}
