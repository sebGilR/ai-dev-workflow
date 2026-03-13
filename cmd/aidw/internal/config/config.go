package config

import (
	"os"

	"aidw/cmd/aidw/internal/util"
)

const (
	// Deprecated: use DefaultAdversarial* constants instead.
	DefaultGeminiModel   = "gemini-2.5-pro"
	DefaultGeminiTimeout = 120 // seconds

	DefaultAdversarialProvider = "gemini"
	DefaultAdversarialModel    = "gemini-2.5-pro"
	DefaultAdversarialTimeout  = 120 // seconds
)

// Config holds all runtime configuration read from environment variables.
type Config struct {
	// Deprecated: use AdversarialReview/AdversarialSet instead.
	// Kept for backward compatibility; reflects AIDW_GEMINI_REVIEW.
	GeminiModel   string
	GeminiTimeout int  // seconds; clamped [10, 600]
	GeminiReview  bool // AIDW_GEMINI_REVIEW=1 → true
	GeminiSet     bool // whether AIDW_GEMINI_REVIEW was explicitly set

	// Provider-agnostic adversarial review fields.
	// New code should read these instead of the Gemini* fields.
	AdversarialReview   bool   // AIDW_ADVERSARIAL_REVIEW=1 (or legacy AIDW_GEMINI_REVIEW=1)
	AdversarialSet      bool   // whether AIDW_ADVERSARIAL_REVIEW was explicitly set
	AdversarialProvider string // "gemini" | "copilot" | "codex"
	AdversarialModel    string
	AdversarialTimeout  int // seconds; clamped [10, 600]
}

// ResolvedProvider returns the effective adversarial review provider name.
func (c Config) ResolvedProvider() string {
	if c.AdversarialProvider != "" {
		return c.AdversarialProvider
	}
	return DefaultAdversarialProvider
}

// Load reads configuration from environment variables.
// Priority: AIDW_ADVERSARIAL_* > AIDW_GEMINI_* (legacy) > defaults.
func Load() Config {
	// Legacy Gemini vars
	geminiReviewRaw, geminiReviewSet := os.LookupEnv("AIDW_GEMINI_REVIEW")
	if geminiReviewSet && geminiReviewRaw == "" {
		geminiReviewSet = false // treat empty as unset
	}
	geminiModel := os.Getenv("AIDW_GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = DefaultGeminiModel
	}
	geminiTimeout := util.ParseIntEnv("AIDW_GEMINI_TIMEOUT", DefaultGeminiTimeout)
	geminiTimeout = clampTimeout(geminiTimeout)

	// New adversarial vars
	advReviewRaw, advReviewSet := os.LookupEnv("AIDW_ADVERSARIAL_REVIEW")
	if advReviewSet && advReviewRaw == "" {
		advReviewSet = false // treat empty as unset
	}
	advProvider := os.Getenv("AIDW_ADVERSARIAL_PROVIDER")
	if advProvider == "" {
		advProvider = DefaultAdversarialProvider
	}
	advModel := os.Getenv("AIDW_ADVERSARIAL_MODEL")
	advTimeoutRaw := util.ParseIntEnv("AIDW_ADVERSARIAL_TIMEOUT", 0)

	// Resolve adversarial enabled: new var takes precedence over legacy.
	var adversarialReview bool
	var adversarialSet bool
	if advReviewSet {
		adversarialReview = advReviewRaw == "1"
		adversarialSet = true
	} else if geminiReviewSet {
		adversarialReview = geminiReviewRaw == "1"
		adversarialSet = false // set via legacy var only
	}

	// Resolve model: new var > legacy gemini model (when provider is gemini) > default.
	if advModel == "" {
		if advProvider == "gemini" {
			advModel = geminiModel
		} else {
			advModel = DefaultAdversarialModel
		}
	}

	// Resolve timeout: new var > legacy gemini timeout > default.
	if advTimeoutRaw <= 0 {
		advTimeoutRaw = geminiTimeout
	}
	advTimeoutRaw = clampTimeout(advTimeoutRaw)

	return Config{
		GeminiModel:   geminiModel,
		GeminiTimeout: geminiTimeout,
		GeminiReview:  geminiReviewRaw == "1",
		GeminiSet:     geminiReviewSet,

		AdversarialReview:   adversarialReview,
		AdversarialSet:      adversarialSet,
		AdversarialProvider: advProvider,
		AdversarialModel:    advModel,
		AdversarialTimeout:  advTimeoutRaw,
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
