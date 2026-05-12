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
	ReplicateAPIToken   string // required
	CloudinaryCloudName string // required
	CloudinaryAPIKey    string // required
	CloudinaryAPISecret string // required
	DBPath              string // default: ./showcaster.db
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
		{"REPLICATE_API_TOKEN", &cfg.ReplicateAPIToken},
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

	return cfg, nil
}
