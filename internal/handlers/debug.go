package handlers

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"showcaster-be/internal/clients"
	"showcaster-be/internal/dto"
	"showcaster-be/internal/models"
)

// DebugHandler exposes debug-only endpoints for development use.
type DebugHandler struct {
	PromptClient *clients.OpenAIClient
	Validator    *validator.Validate
}

// generatePromptRequest is the request body for POST /api/v1/debug/prompt.
type generatePromptRequest struct {
	StepName        string `json:"stepName"        validate:"required,oneof=Hook Problem Solution Closure"`
	ProductName     string `json:"productName"     validate:"required,max=200"`
	ProductCategory string `json:"productCategory" validate:"required,oneof=beauty fashion electronics health"`
	TargetAudience  string `json:"targetAudience"  validate:"required,oneof=man woman children unisex"`
	Orientation     string `json:"orientation"     validate:"required,oneof=portrait landscape square"`
	Resolution      string `json:"resolution"      validate:"required,oneof=720p 1080p 4k"`
	ModelImageURL   string `json:"modelImageUrl"   validate:"omitempty,url"`
}

// GeneratePrompt handles POST /api/v1/debug/prompt.
// It calls the configured prompt client (deepseek-v4-flash via DashScope) and
// returns the structured ScenePrompt JSON for the requested pipeline step.
func (h *DebugHandler) GeneratePrompt(c *fiber.Ctx) error {
	if h.PromptClient == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			dto.Fail("prompt client not configured (DASHSCOPE_API_KEY missing)"),
		)
	}

	var req generatePromptRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail(err.Error()))
	}

	// Build a minimal Job from the request fields so we can reuse GenerateScenePrompt.
	job := models.Job{
		ProductName:     req.ProductName,
		ProductCategory: req.ProductCategory,
		TargetAudience:  req.TargetAudience,
		Orientation:     req.Orientation,
		Resolution:      req.Resolution,
		ModelImageURL:   req.ModelImageURL,
	}

	scene, err := h.PromptClient.GenerateScenePrompt(c.Context(), job, req.StepName)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("prompt generation failed: " + err.Error()))
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(scene))
}
