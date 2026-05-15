package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"showcaster-be/internal/models"
)

// ScenePrompt is the structured JSON that GPT generates for each pipeline step.
// It is used both as the prompt sent to Veo and as the debug preview in the frontend.
type ScenePrompt struct {
	SceneID      string        `json:"scene_id"`
	Title        string        `json:"title"`
	Duration     int           `json:"duration"`
	VisualPrompt string        `json:"visual_prompt"`
	VoiceOver    string        `json:"voice_over"`
	VoiceQuality string        `json:"voice_quality"`
	MotionProfile MotionProfile `json:"motion_profile"`
	IsGeneratingVideo bool     `json:"is_generating_video"`
}

// MotionProfile describes camera and movement characteristics for the scene.
type MotionProfile struct {
	CameraAngle    string   `json:"camera_angle"`
	Intensity      float64  `json:"intensity"`
	MicroMovements []string `json:"micro_movements"`
}

// OpenAIClient generates structured scene prompts using a chat-completions model.
// It supports both the OpenAI API and any OpenAI-compatible endpoint (e.g. DashScope).
type OpenAIClient struct {
	client    openai.Client
	modelName string
	logger    *slog.Logger
}

// NewOpenAIClient creates an OpenAIClient backed by GPT-4.1-nano on the OpenAI API.
func NewOpenAIClient(apiKey string) *OpenAIClient {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAIClient{
		client:    client,
		modelName: "gpt-4.1-nano",
		logger:    slog.Default(),
	}
}

// NewDashScopePromptClient creates an OpenAIClient that calls the DashScope
// OpenAI-compatible endpoint using the deepseek-v4-flash model.
// The same DASHSCOPE_API_KEY used for Wan video generation is reused here.
func NewDashScopePromptClient(apiKey string) *OpenAIClient {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL("https://dashscope-intl.aliyuncs.com/compatible-mode/v1"),
	)
	return &OpenAIClient{
		client:    client,
		modelName: "deepseek-v4-flash",
		logger:    slog.Default(),
	}
}

// GenerateScenePrompt calls the configured chat model to produce a structured
// ScenePrompt for the given pipeline step and job metadata.
func (c *OpenAIClient) GenerateScenePrompt(ctx context.Context, job models.Job, stepName string) (*ScenePrompt, error) {
	c.logger.Info("openai: generating scene prompt", "step", stepName, "product", job.ProductName, "model", c.modelName)

	systemPrompt := `You are a professional AI video production director specializing in affiliate marketing videos.
Your task is to generate a highly specific, structured JSON scene prompt for a video generation AI (Google Veo).

The JSON must follow this exact schema:
{
  "scene_id": "<step>_scene_01",
  "title": "<descriptive title>",
  "duration": 8,
  "visual_prompt": "<detailed cinematic prompt for Veo — RAW photography style, NO TEXT, NO LOGOS>",
  "voice_over": "<natural, conversational Indonesian language voice-over script for this scene>",
  "voice_quality": "Consistent Voice Tone, Same speaker through the video, Fixed pitch and stable speed, Clear studio Voice",
  "motion_profile": {
    "camera_angle": "<appropriate camera angle and shot type>",
    "intensity": <float 0.1-0.5 for subtle motion>,
    "micro_movements": ["<movement 1>", "<movement 2>", "<movement 3>", "<movement 4>"]
  },
  "is_generating_video": true
}

Rules for visual_prompt:
- Start with "RAW photography, high detail, realistic style"
- Describe the subject, product, and action clearly
- Include micro-movements in the description
- End with: "NO TEXT, NO WORDS, NO TYPOGRAPHY, NO LOGOS. The background, environment, and lighting must remain exactly the same as the reference image, maintaining a bright and clean aesthetic."
- Do NOT include any camera instructions in visual_prompt (those go in motion_profile)

Rules for voice_over:
- Write in natural, conversational Indonesian (Bahasa Indonesia)
- Match the scene's emotional tone
- Keep it concise (2-3 sentences max)
- Make it persuasive and relatable for the target audience

Respond with ONLY the JSON object, no markdown, no explanation.`

	userPrompt := buildGPTUserPrompt(stepName, job)

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.modelName,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		Temperature: openai.Float(0.7),
		MaxTokens:   openai.Int(1024),
	})
	if err != nil {
		return nil, fmt.Errorf("openai: generate scene prompt: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)
	// Strip markdown code fences if present
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var scene ScenePrompt
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		c.logger.Error("openai: failed to parse scene JSON", "step", stepName, "raw", raw, "error", err)
		return nil, fmt.Errorf("openai: parse scene prompt JSON: %w (raw: %s)", err, raw)
	}

	c.logger.Info("openai: scene prompt generated", "step", stepName, "sceneId", scene.SceneID, "model", c.modelName)
	return &scene, nil
}

// buildGPTUserPrompt constructs the user message for GPT based on the step and job.
func buildGPTUserPrompt(stepName string, job models.Job) string {
	stepDescriptions := map[string]string{
		"Hook":     "attention-grabbing opening that immediately showcases the product and hooks the viewer in the first 2 seconds",
		"Problem":  "relatable scene showing the everyday problem or pain point that this product solves for the target audience",
		"Solution": "satisfying product reveal showing the target audience using the product and experiencing a positive transformation",
		"Closure":  "compelling call-to-action closing scene with the product prominently featured, driving the viewer to purchase",
	}

	stepDesc := stepDescriptions[stepName]
	if stepDesc == "" {
		stepDesc = "product showcase scene"
	}

	return fmt.Sprintf(`Generate a scene prompt for the "%s" step of a 4-part affiliate marketing video.

Product Details:
- Product Name: %s
- Category: %s
- Target Audience: %s
- Video Orientation: %s
- Resolution: %s

Scene Purpose: %s

The scene should feel authentic, cinematic, and optimized for %s social media content.
The target audience is %s, so the tone, style, and voice-over language should resonate with them.
Use Indonesian language for the voice_over.`,
		stepName,
		job.ProductName,
		job.ProductCategory,
		job.TargetAudience,
		job.Orientation,
		job.Resolution,
		stepDesc,
		orientationToSocialMedia(job.Orientation),
		job.TargetAudience,
	)
}

func orientationToSocialMedia(orientation string) string {
	switch orientation {
	case "portrait":
		return "TikTok/Instagram Reels (9:16)"
	case "square":
		return "Instagram Feed (1:1)"
	default:
		return "YouTube (16:9)"
	}
}
