package config

import (
	"os"

	"aidw/cmd/aidw/internal/util"
)

const (
	// Default 2026 Model Tiers
	DefaultFrontierModel  = "gemini-2.5-ultra"
	DefaultEfficientModel = "gemini-2.5-flash"

	// Adversarial Review Defaults
	DefaultAdversarialProvider = "gemini"
	DefaultAdversarialModel    = "gemini-2.5-ultra"
	DefaultAdversarialTimeout  = 120 // seconds

	// Legacy Defaults (Backward Compatibility)
	DefaultGeminiModel   = "gemini-2.5-ultra"
	DefaultGeminiTimeout = 120 // seconds
)

// Config holds all runtime configuration read from environment variables.
type Config struct {
	// Provider-agnostic adversarial review fields.
	AdversarialReview   bool
	AdversarialSet      bool
	AdversarialProvider string
	AdversarialModel    string
	AdversarialTimeout  int

	// Tiered model configuration
	FrontierModel  string // AIDW_FRONTIER_MODEL
	EfficientModel string // AIDW_EFFICIENT_MODEL

	// Legacy fields (deprecated)
	GeminiModel   string
	GeminiTimeout int
	GeminiReview  bool
	GeminiSet     bool
}

// ResolvedProvider returns the effective adversarial review provider name.
func (c Config) ResolvedProvider() string {
	if c.AdversarialProvider != "" {
		return c.AdversarialProvider
	}
	return DefaultAdversarialProvider
}

// Load reads configuration from environment variables.
func Load() Config {
	// 1. Tiered Model vars (2026 standard)
	frontierModel := os.Getenv("AIDW_FRONTIER_MODEL")
	if frontierModel == "" {
		frontierModel = DefaultFrontierModel
	}
	efficientModel := os.Getenv("AIDW_EFFICIENT_MODEL")
	if efficientModel == "" {
		efficientModel = DefaultEfficientModel
	}

	// 2. Adversarial Review vars
	advReviewRaw, advReviewSet := os.LookupEnv("AIDW_ADVERSARIAL_REVIEW")
	advProvider := os.Getenv("AIDW_ADVERSARIAL_PROVIDER")
	if advProvider == "" {
		advProvider = DefaultAdversarialProvider
	}
	advModel := os.Getenv("AIDW_ADVERSARIAL_MODEL")
	advTimeoutRaw := util.ParseIntEnv("AIDW_ADVERSARIAL_TIMEOUT", 0)

	// Legacy fallback handling
	geminiReviewRaw, geminiReviewSet := os.LookupEnv("AIDW_GEMINI_REVIEW")
	geminiModel := os.Getenv("AIDW_GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = DefaultGeminiModel
	}
	geminiTimeout := util.ParseIntEnv("AIDW_GEMINI_TIMEOUT", DefaultGeminiTimeout)

	var adversarialReview bool
	var adversarialSet bool
	if advReviewSet {
		adversarialReview = advReviewRaw == "1"
		adversarialSet = true
	} else if geminiReviewSet {
		adversarialReview = geminiReviewRaw == "1"
		adversarialSet = false
	}

	if advModel == "" && advProvider == "gemini" {
		advModel = geminiModel
	}
	if advTimeoutRaw <= 0 {
		advTimeoutRaw = geminiTimeout
	}

	return Config{
		AdversarialReview:   adversarialReview,
		AdversarialSet:      adversarialSet,
		AdversarialProvider: advProvider,
		AdversarialModel:    advModel,
		AdversarialTimeout:  clampTimeout(advTimeoutRaw),

		FrontierModel:  frontierModel,
		EfficientModel: efficientModel,

		GeminiModel:   geminiModel,
		GeminiTimeout: geminiTimeout,
		GeminiReview:  geminiReviewRaw == "1",
		GeminiSet:     geminiReviewSet,
	}
}

func clampTimeout(t int) int {
	if t < 10 {
		return 10
	}
	if t > 600 {
		return 600
	}
	return t
}
