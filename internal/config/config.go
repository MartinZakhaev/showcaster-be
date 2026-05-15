package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// Config holds all runtime configuration values loaded from environment variables.
type Config struct {
	Port                int    // default: 8080
	JWTSecret           string // required
	ReplicateAPIToken   string // optional (kept for reference, not used when Veo is active)
	CloudinaryCloudName string // required
	CloudinaryAPIKey    string // required
	CloudinaryAPISecret string // required
	DBPath              string // default: ./showcaster.db
	// Google Vertex AI / Veo
	GoogleProjectID string // required for Veo (Vertex AI)
	GoogleLocation  string // default: us-central1
	GoogleAIAPIKey  string // AI Studio API key (simpler alternative to Vertex AI)
	// OpenAI — GPT-4.1-nano for structured prompt generation
	OpenAIAPIKey string // optional; if absent, falls back to template prompts
	// Video provider: "veo" (default) or "wan"
	VideoProvider string
	// Alibaba Cloud Model Studio (DashScope) API key for Wan 2.7
	DashScopeAPIKey string
}

// Load reads configuration from environment variables, applies defaults, validates
// required fields and constraints, and returns a populated Config or an error.
// Callers are responsible for exiting the process on error.
func Load() (*Config, error) {
	cfg := &Config{}

	// --- PORT ---
	portStr := os.Getenv("PORT")
	if portStr == "" {
		cfg.Port = 8080
	} else {
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			slog.Error("invalid PORT value; must be an integer in range 1–65535", "value", portStr)
			return nil, fmt.Errorf("invalid PORT value %q: must be an integer in range 1–65535", portStr)
		}
		cfg.Port = port
	}

	// --- DB_PATH ---
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		cfg.DBPath = "./showcaster.db"
	} else {
		cfg.DBPath = dbPath
	}

	// --- Required secrets ---
	required := []struct {
		envKey string
		dest   *string
	}{
		{"JWT_SECRET", &cfg.JWTSecret},
		{"CLOUDINARY_CLOUD_NAME", &cfg.CloudinaryCloudName},
		{"CLOUDINARY_API_KEY", &cfg.CloudinaryAPIKey},
		{"CLOUDINARY_API_SECRET", &cfg.CloudinaryAPISecret},
	}

	for _, r := range required {
		val := os.Getenv(r.envKey)
		if val == "" {
			slog.Error("required environment variable is missing or empty", "variable", r.envKey)
			return nil, fmt.Errorf("required environment variable %q is missing or empty", r.envKey)
		}
		*r.dest = val
	}

	// --- Optional ---
	cfg.ReplicateAPIToken = os.Getenv("REPLICATE_API_TOKEN")
	cfg.GoogleProjectID = os.Getenv("GOOGLE_PROJECT_ID")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY") // optional
	cfg.DashScopeAPIKey = os.Getenv("DASHSCOPE_API_KEY")

	provider := os.Getenv("VIDEO_PROVIDER")
	if provider == "" {
		provider = "veo"
	}
	cfg.VideoProvider = provider

	switch cfg.VideoProvider {
	case "veo":
		// GOOGLE_AI_API_KEY is required only when using the Veo provider
		googleAIAPIKey := os.Getenv("GOOGLE_AI_API_KEY")
		if googleAIAPIKey == "" {
			slog.Error("required environment variable is missing or empty (required when VIDEO_PROVIDER=veo)", "variable", "GOOGLE_AI_API_KEY")
			return nil, fmt.Errorf("required environment variable %q is missing or empty", "GOOGLE_AI_API_KEY")
		}
		cfg.GoogleAIAPIKey = googleAIAPIKey
	case "wan":
		if cfg.DashScopeAPIKey == "" {
			slog.Error("required environment variable is missing or empty (required when VIDEO_PROVIDER=wan)", "variable", "DASHSCOPE_API_KEY")
			return nil, fmt.Errorf("required environment variable %q is missing or empty (required when VIDEO_PROVIDER=wan)", "DASHSCOPE_API_KEY")
		}
	default:
		slog.Error("invalid VIDEO_PROVIDER value; must be \"veo\" or \"wan\"", "value", cfg.VideoProvider)
		return nil, fmt.Errorf("invalid VIDEO_PROVIDER value %q: must be \"veo\" or \"wan\"", cfg.VideoProvider)
	}

	location := os.Getenv("GOOGLE_LOCATION")
	if location == "" {
		cfg.GoogleLocation = "us-central1"
	} else {
		cfg.GoogleLocation = location
	}

	return cfg, nil
}
