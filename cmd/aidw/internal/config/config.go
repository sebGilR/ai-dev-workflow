package config

import (
	"os"

	"aidw/cmd/aidw/internal/util"
)

const (
	DefaultGeminiModel   = "gemini-2.5-pro"
	DefaultGeminiTimeout = 120 // seconds
)

// Config holds all runtime configuration read from environment variables.
type Config struct {
	GeminiModel   string
	GeminiTimeout int  // seconds; clamped [10, 600]
	GeminiReview  bool // AIDW_GEMINI_REVIEW=1 → true; =0 → false; unset → ask user
	GeminiSet     bool // whether AIDW_GEMINI_REVIEW was explicitly set
}

// Load reads configuration from environment variables.
func Load() Config {
	reviewRaw, reviewSet := os.LookupEnv("AIDW_GEMINI_REVIEW")

	model := os.Getenv("AIDW_GEMINI_MODEL")
	if model == "" {
		model = DefaultGeminiModel
	}

	return Config{
		GeminiModel:   model,
		GeminiTimeout: util.ParseIntEnv("AIDW_GEMINI_TIMEOUT", DefaultGeminiTimeout),
		GeminiReview:  reviewRaw == "1",
		GeminiSet:     reviewSet,
	}
}
