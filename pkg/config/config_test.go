package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.MaxSessionCostUSD != 2.00 {
		t.Errorf("MaxSessionCostUSD = %v, want 2.00", c.MaxSessionCostUSD)
	}
	if c.DefaultTokenizerEncoding != "cl100k_base" {
		t.Errorf("DefaultTokenizerEncoding = %q", c.DefaultTokenizerEncoding)
	}
	if c.LoopDetectionThreshold != 3 {
		t.Errorf("LoopDetectionThreshold = %d, want 3", c.LoopDetectionThreshold)
	}
	if !c.CacheAlignmentEnabled {
		t.Error("CacheAlignmentEnabled should default to true")
	}
	if c.MaxSessionTokens != 500000 {
		t.Errorf("MaxSessionTokens = %d", c.MaxSessionTokens)
	}
	if c.Pricing.PromptPerMillion != 3.00 {
		t.Errorf("prompt price = %v", c.Pricing.PromptPerMillion)
	}
	if c.Pricing.CachedPromptPerMillion != 0.30 {
		t.Errorf("cached price = %v", c.Pricing.CachedPromptPerMillion)
	}
	if c.Pricing.CompletionPerMillion != 15.00 {
		t.Errorf("completion price = %v", c.Pricing.CompletionPerMillion)
	}
}

func TestCostForTokens(t *testing.T) {
	c := Default()
	got := c.CostForTokens(1_000_000, 0, 0)
	if got != 3.00 {
		t.Errorf("cost(1M prompt) = %v, want 3.00", got)
	}
	got = c.CostForTokens(0, 1_000_000, 0)
	if got != 15.00 {
		t.Errorf("cost(1M completion) = %v, want 15.00", got)
	}
	got = c.CostForTokens(1_000_000, 0, 1_000_000)
	if got < 0.29 || got > 0.31 {
		t.Errorf("cost(1M cached) = %v, want ~0.30", got)
	}
	got = c.CostForTokens(1_000_000, 200_000, 500_000)
	want := 0.5*3.00 + 0.5*0.30 + 0.2*15.00
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("cost(mixed) = %v, want ~%v", got, want)
	}
	got = c.CostForTokens(0, 0, 0)
	if got != 0 {
		t.Errorf("cost(0,0,0) = %v, want 0", got)
	}
}

func withTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIST_CONFIG_DIR", dir)
	t.Cleanup(func() {
		ResetConfigDir()
	})
	return dir
}

func TestSaveAndLoad(t *testing.T) {
	withTempConfigDir(t)

	c := Default()
	c.MaxSessionCostUSD = 7.50
	c.LoopDetectionThreshold = 9
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir, _ := ConfigDir()
	expected := filepath.Join(dir, "config.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file at %s: %v", expected, err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.MaxSessionCostUSD != 7.50 {
		t.Errorf("loaded MaxSessionCostUSD = %v", loaded.MaxSessionCostUSD)
	}
	if loaded.LoopDetectionThreshold != 9 {
		t.Errorf("loaded LoopDetectionThreshold = %d", loaded.LoopDetectionThreshold)
	}
	if !loaded.CacheAlignmentEnabled {
		t.Error("cache alignment flag lost")
	}
}

func TestLoadCreatesDefaultWhenMissing(t *testing.T) {
	dir := withTempConfigDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxSessionCostUSD != Default().MaxSessionCostUSD {
		t.Error("expected defaults when config missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Error("expected default config file to be created")
	}
}

func TestConfigDirUsesEnv(t *testing.T) {
	dir := withTempConfigDir(t)
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if got != dir {
		t.Errorf("ConfigDir = %q, want %q", got, dir)
	}
}

func TestConfigPaths(t *testing.T) {
	dir := withTempConfigDir(t)

	cp, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cp) != dir {
		t.Errorf("ConfigPath dir = %q, want %q", filepath.Dir(cp), dir)
	}
	sp, err := SessionsPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(sp) != dir {
		t.Errorf("SessionsPath dir = %q, want %q", filepath.Dir(sp), dir)
	}
}