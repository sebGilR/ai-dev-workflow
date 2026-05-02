package config

import (
	"testing"
)

func TestLoad_AdversarialDefaults(t *testing.T) {
	clearEnv(t)
	cfg := Load()

	if cfg.AdversarialProvider != DefaultAdversarialProvider {
		t.Errorf("AdversarialProvider = %q, want %q", cfg.AdversarialProvider, DefaultAdversarialProvider)
	}
	if cfg.AdversarialModel != DefaultAdversarialModel {
		t.Errorf("AdversarialModel = %q, want %q", cfg.AdversarialModel, DefaultAdversarialModel)
	}
	if cfg.AdversarialTimeout != DefaultAdversarialTimeout {
		t.Errorf("AdversarialTimeout = %d, want %d", cfg.AdversarialTimeout, DefaultAdversarialTimeout)
	}
	if cfg.AdversarialReview {
		t.Error("AdversarialReview should be false when unset")
	}
	if cfg.AdversarialSet {
		t.Error("AdversarialSet should be false when unset")
	}
}

func clearEnv(t *testing.T) {
	vars := []string{
		"AIDW_ADVERSARIAL_REVIEW",
		"AIDW_ADVERSARIAL_PROVIDER",
		"AIDW_ADVERSARIAL_MODEL",
		"AIDW_ADVERSARIAL_TIMEOUT",
		"AIDW_GEMINI_REVIEW",
		"AIDW_GEMINI_MODEL",
		"AIDW_GEMINI_TIMEOUT",
		"AIDW_FRONTIER_MODEL",
		"AIDW_EFFICIENT_MODEL",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}

func TestLoad_AdversarialVarsTakePrecedence(t *testing.T) {
	t.Setenv("AIDW_ADVERSARIAL_REVIEW", "1")
	t.Setenv("AIDW_ADVERSARIAL_PROVIDER", "codex")
	t.Setenv("AIDW_ADVERSARIAL_MODEL", "gpt-4o")
	t.Setenv("AIDW_ADVERSARIAL_TIMEOUT", "200")
	t.Setenv("AIDW_GEMINI_REVIEW", "0") // legacy var says disabled — should be ignored
	t.Setenv("AIDW_GEMINI_MODEL", "gemini-old")

	cfg := Load()

	if !cfg.AdversarialReview {
		t.Error("AdversarialReview should be true")
	}
	if !cfg.AdversarialSet {
		t.Error("AdversarialSet should be true when AIDW_ADVERSARIAL_REVIEW is set")
	}
	if cfg.AdversarialProvider != "codex" {
		t.Errorf("AdversarialProvider = %q, want codex", cfg.AdversarialProvider)
	}
	if cfg.AdversarialModel != "gpt-4o" {
		t.Errorf("AdversarialModel = %q, want gpt-4o", cfg.AdversarialModel)
	}
	if cfg.AdversarialTimeout != 200 {
		t.Errorf("AdversarialTimeout = %d, want 200", cfg.AdversarialTimeout)
	}
}

func TestLoad_LegacyGeminiFallback(t *testing.T) {
	// When only AIDW_GEMINI_* are set, adversarial fields should inherit them.
	t.Setenv("AIDW_ADVERSARIAL_REVIEW", "")
	t.Setenv("AIDW_ADVERSARIAL_PROVIDER", "")
	t.Setenv("AIDW_ADVERSARIAL_MODEL", "")
	t.Setenv("AIDW_ADVERSARIAL_TIMEOUT", "")
	t.Setenv("AIDW_GEMINI_REVIEW", "1")
	t.Setenv("AIDW_GEMINI_MODEL", "gemini-1.5-pro")
	t.Setenv("AIDW_GEMINI_TIMEOUT", "180")

	cfg := Load()

	if !cfg.AdversarialReview {
		t.Error("AdversarialReview should be true via legacy AIDW_GEMINI_REVIEW=1")
	}
	if cfg.AdversarialSet {
		t.Error("AdversarialSet should be false when only legacy var is set")
	}
	if cfg.GeminiReview != cfg.AdversarialReview {
		t.Error("GeminiReview and AdversarialReview should agree when only legacy vars are set")
	}
	// Model and timeout should inherit from legacy vars for gemini provider.
	if cfg.AdversarialModel != "gemini-1.5-pro" {
		t.Errorf("AdversarialModel = %q, want gemini-1.5-pro (inherited from legacy)", cfg.AdversarialModel)
	}
	if cfg.AdversarialTimeout != 180 {
		t.Errorf("AdversarialTimeout = %d, want 180 (inherited from legacy)", cfg.AdversarialTimeout)
	}
}

func TestLoad_LegacyDisabledDoesNotEnableAdversarial(t *testing.T) {
	t.Setenv("AIDW_ADVERSARIAL_REVIEW", "")
	t.Setenv("AIDW_GEMINI_REVIEW", "0")

	cfg := Load()

	if cfg.AdversarialReview {
		t.Error("AdversarialReview should be false when AIDW_GEMINI_REVIEW=0")
	}
}

func TestResolvedProvider_Default(t *testing.T) {
	cfg := Config{}
	if cfg.ResolvedProvider() != DefaultAdversarialProvider {
		t.Errorf("ResolvedProvider() = %q, want %q", cfg.ResolvedProvider(), DefaultAdversarialProvider)
	}
}

func TestResolvedProvider_Override(t *testing.T) {
	cfg := Config{AdversarialProvider: "copilot"}
	if cfg.ResolvedProvider() != "copilot" {
		t.Errorf("ResolvedProvider() = %q, want copilot", cfg.ResolvedProvider())
	}
}

func TestLoad_TimeoutClamping(t *testing.T) {
	t.Setenv("AIDW_ADVERSARIAL_TIMEOUT", "5") // below minimum
	cfg := Load()
	if cfg.AdversarialTimeout != 10 {
		t.Errorf("AdversarialTimeout = %d, want 10 (clamped from 5)", cfg.AdversarialTimeout)
	}

	t.Setenv("AIDW_ADVERSARIAL_TIMEOUT", "9999") // above maximum
	cfg = Load()
	if cfg.AdversarialTimeout != 600 {
		t.Errorf("AdversarialTimeout = %d, want 600 (clamped from 9999)", cfg.AdversarialTimeout)
	}
}
