package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"showcaster-be/internal/dto"
)

// HealthHandler handles the health check endpoint.
type HealthHandler struct {
	DB *gorm.DB
}

// Health handles GET /health.
//
//	@Summary		Health check
//	@Description	Verifies that the service is running and the database is reachable.
//	@Description	Attempts a DB ping with a 2-second deadline; always responds within 3 seconds.
//	@Description
//	@Description	Suitable for use as a liveness/readiness probe in container orchestration.
//	@Tags			System
//	@Produce		json
//	@Success		200	{object}	dto.HealthResponse	"Service and database are healthy"
//	@Failure		503	{object}	dto.ErrorResponse	"Database ping failed or timed out"
//	@Router			/health [get]
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	sqlDB, err := h.DB.WithContext(ctx).DB()
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			dto.Fail("database unavailable: " + err.Error()),
		)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			dto.Fail("database ping failed: " + err.Error()),
		)
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(fiber.Map{"status": "ok"}))
}
