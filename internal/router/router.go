package router

import (
	scalar "github.com/MarceloPetrucio/go-scalar-api-reference"
	fiberprometheus "github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/swaggo/swag"

	"showcaster-be/internal/handlers"
	"showcaster-be/internal/middleware"
)

// Setup registers all routes and middleware on the provided Fiber app.
//
// Unversioned routes (infrastructure):
//
//	GET  /health            — liveness / readiness probe
//	GET  /metrics           — Prometheus metrics (HTTP + Go runtime)
//	GET  /docs              — Scalar API UI
//	GET  /docs/openapi.json — raw OpenAPI spec
//
// Versioned public routes (no JWT required):
//
//	POST /api/v1/auth/register
//	POST /api/v1/auth/login
//	POST /api/v1/auth/verify-otp
//	POST /api/v1/auth/resend-otp
//
// Versioned protected routes (JWT required):
//
//	POST   /api/v1/upload/image
//	POST   /api/v1/jobs/generate
//	GET    /api/v1/jobs
//	GET    /api/v1/jobs/:id
//	DELETE /api/v1/jobs/:id
func Setup(
	app *fiber.App,
	authH *handlers.AuthHandler,
	jobH *handlers.JobHandler,
	uploadH *handlers.UploadHandler,
	healthH *handlers.HealthHandler,
	debugH *handlers.DebugHandler,
	jwtSecret string,
) {
	// CORS must be the very first middleware so preflight OPTIONS requests
	// get the correct headers before any other middleware can intercept them.
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
		MaxAge:       86400,
	}))

	// Global panic recovery — returns HTTP 500 on any unhandled panic.
	app.Use(recover.New())

	// Prometheus middleware — records per-route HTTP metrics.
	prom := fiberprometheus.New("showcaster_be")
	prom.RegisterAt(app, "/metrics")
	app.Use(prom.Middleware)

	// ── Infrastructure routes (unversioned) ──────────────────────────────────

	app.Get("/health", healthH.Health)

	// Scalar API docs UI.
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

	// Raw OpenAPI JSON spec.
	app.Get("/docs/openapi.json", func(c *fiber.Ctx) error {
		spec, err := swag.ReadDoc()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("failed to load OpenAPI spec")
		}
		c.Set("Content-Type", "application/json")
		return c.SendString(spec)
	})

	// ── API v1 ───────────────────────────────────────────────────────────────

	v1 := app.Group("/api/v1")

	// Public auth routes — no JWT required.
	auth := v1.Group("/auth")
	auth.Post("/register", authH.Register)
	auth.Post("/login", authH.Login)
	auth.Post("/verify-otp", authH.VerifyOTP)
	auth.Post("/resend-otp", authH.ResendOTP)

	// Protected routes — JWT middleware applied to the group.
	protected := v1.Group("", middleware.JWTMiddleware(jwtSecret))

	protected.Post("/upload/image", uploadH.UploadImage)

	protected.Post("/jobs/generate", jobH.CreateJob)
	protected.Get("/jobs", jobH.ListJobs)
	protected.Get("/jobs/:id", jobH.GetJob)
	protected.Post("/jobs/:id/cancel", jobH.CancelJob)
	protected.Delete("/jobs/:id", jobH.DeleteJob)

	// Debug routes — protected by JWT to avoid accidental public exposure.
	protected.Post("/debug/prompt", debugH.GeneratePrompt)
}
