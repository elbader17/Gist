// Package config loads and persists the Gist runtime configuration.
//
// Configuration is stored as JSON at ~/.config/tokenless/config.json. Override
// the directory at runtime with the TOKENLESS_CONFIG_DIR environment variable.
//
// # Schema
//
//	max_session_cost_usd        default 2.00
//	max_session_tokens          default 500000
//	default_tokenizer_encoding  default "cl100k_base"
//	loop_detection_threshold    default 3
//	cache_alignment_enabled     default true
//	pricing.prompt_per_million          default 3.00
//	pricing.completion_per_million      default 15.00
//	pricing.cached_prompt_per_million   default 0.30
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Pricing describes per-million-token rates for cost estimation.
type Pricing struct {
	PromptPerMillion       float64 `json:"prompt_per_million"`
	CompletionPerMillion   float64 `json:"completion_per_million"`
	CachedPromptPerMillion float64 `json:"cached_prompt_per_million"`
}

// Config is the full runtime configuration for Gist.
type Config struct {
	MaxSessionCostUSD        float64 `json:"max_session_cost_usd"`
	MaxSessionTokens         int64   `json:"max_session_tokens"`
	DefaultTokenizerEncoding string  `json:"default_tokenizer_encoding"`
	LoopDetectionThreshold   int     `json:"loop_detection_threshold"`
	CacheAlignmentEnabled    bool    `json:"cache_alignment_enabled"`
	Pricing                  Pricing `json:"pricing"`
}

// Default returns a Config with sensible defaults from the spec.
func Default() *Config {
	return &Config{
		MaxSessionCostUSD:        2.00,
		MaxSessionTokens:         500000,
		DefaultTokenizerEncoding: "cl100k_base",
		LoopDetectionThreshold:   3,
		CacheAlignmentEnabled:    true,
		Pricing: Pricing{
			PromptPerMillion:       3.00,
			CompletionPerMillion:   15.00,
			CachedPromptPerMillion: 0.30,
		},
	}
}

// ConfigDir returns the directory where config.json and sessions.json live.
//
// Resolution order:
//
//  1. SetConfigDir override (used by tests).
//  2. TOKENLESS_CONFIG_DIR environment variable.
//  3. $HOME/.config/tokenless
func ConfigDir() (string, error) {
	configDirMu.RLock()
	override := configDirOverride
	configDirMu.RUnlock()

	if override != "" {
		return override, nil
	}
	if dir := os.Getenv("TOKENLESS_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("unable to determine home directory: " + err.Error())
	}
	return filepath.Join(home, ".config", "tokenless"), nil
}

// ConfigPath returns the absolute path to config.json.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// SessionsPath returns the absolute path to sessions.json.
func SessionsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.json"), nil
}

// Load reads config.json from disk. If the file does not exist, a default
// config is written and returned.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			_ = cfg.Save()
			return cfg, nil
		}
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config to disk atomically (temp file + rename).
func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// CostForTokens estimates cost in USD given token counts.
//
// promptTokens is total prompt tokens. cachedTokens is the subset already in
// the provider cache (charged at the cached rate). The remainder
// (promptTokens - cachedTokens) is charged at the regular prompt rate.
// completionTokens is charged at the completion rate.
func (c *Config) CostForTokens(promptTokens, completionTokens, cachedTokens int64) float64 {
	if promptTokens < cachedTokens {
		cachedTokens = promptTokens
	}
	prompt := float64(promptTokens-cachedTokens) / 1_000_000.0
	cached := float64(cachedTokens) / 1_000_000.0
	completion := float64(completionTokens) / 1_000_000.0
	return prompt*c.Pricing.PromptPerMillion +
		cached*c.Pricing.CachedPromptPerMillion +
		completion*c.Pricing.CompletionPerMillion
}

// SetConfigDir forces a specific directory for the lifetime of the process.
// Intended for tests; production code should rely on ConfigDir env resolution.
func SetConfigDir(dir string) {
	configDirMu.Lock()
	configDirOverride = dir
	configDirMu.Unlock()
}

// ResetConfigDir clears any override set by SetConfigDir.
func ResetConfigDir() {
	configDirMu.Lock()
	configDirOverride = ""
	configDirMu.Unlock()
}

var (
	configDirOverride string
	configDirMu       sync.RWMutex
)