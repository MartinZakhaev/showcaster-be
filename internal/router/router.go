package router

import (
	scalar "github.com/MarceloPetrucio/go-scalar-api-reference"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/swaggo/swag"

	"showcaster-be/internal/handlers"
	"showcaster-be/internal/middleware"
)

// Setup registers all routes and middleware on the provided Fiber app.
//
// Global middleware:
//   - recover.New() catches any unhandled panics and returns HTTP 500.
//
// Public routes (no authentication required):
//
//	POST /api/auth/register
//	POST /api/auth/login
//	POST /api/auth/verify-otp
//	POST /api/auth/resend-otp
//	GET  /health
//	GET  /docs        — Scalar API UI
//	GET  /docs/openapi.json — raw OpenAPI spec
//
// Protected routes (JWT required):
//
//	POST   /api/upload/image
//	POST   /api/jobs/generate
//	GET    /api/jobs
//	GET    /api/jobs/:id
//	DELETE /api/jobs/:id
func Setup(
	app *fiber.App,
	authH *handlers.AuthHandler,
	jobH *handlers.JobHandler,
	uploadH *handlers.UploadHandler,
	healthH *handlers.HealthHandler,
	jwtSecret string,
) {
	// Global panic recovery middleware.
	app.Use(recover.New())

	// Public routes — no JWT required.
	app.Post("/api/auth/register", authH.Register)
	app.Post("/api/auth/login", authH.Login)
	app.Post("/api/auth/verify-otp", authH.VerifyOTP)
	app.Post("/api/auth/resend-otp", authH.ResendOTP)
	app.Get("/health", healthH.Health)

	// Scalar API docs UI — served from the embedded swaggo spec.
	app.Get("/docs", func(c *fiber.Ctx) error {
		spec, err := swag.ReadDoc()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load OpenAPI spec")
		}
		htmlContent, err := scalar.ApiReferenceHTML(&scalar.Options{
			SpecContent: spec,
			CustomOptions: scalar.CustomOptions{
				PageTitle: "Showcaster API",
			},
			DarkMode: true,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to render docs")
		}
		c.Set("Content-Type", "text/html")
		return c.SendString(htmlContent)
	})

	// Serve the raw OpenAPI JSON spec from the embedded swaggo docs.
	app.Get("/docs/openapi.json", func(c *fiber.Ctx) error {
		spec, err := swag.ReadDoc()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load OpenAPI spec")
		}
		c.Set("Content-Type", "application/json")
		return c.SendString(spec)
	})

	// Protected routes — JWT middleware applied to the group.
	api := app.Group("/api", middleware.JWTMiddleware(jwtSecret))

	api.Post("/upload/image", uploadH.UploadImage)

	api.Post("/jobs/generate", jobH.CreateJob)
	api.Get("/jobs", jobH.ListJobs)
	api.Get("/jobs/:id", jobH.GetJob)
	api.Delete("/jobs/:id", jobH.DeleteJob)
}
