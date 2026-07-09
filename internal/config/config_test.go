package config

import (
	"encoding/json"
	"os"
	"testing"
)

// TestUpdateMutatesSharedInstanceInPlace guards the guarantee that makes
// self-configuration mid-conversation possible: Update must mutate the
// exact struct Load already handed out, not swap in a new one, so every
// other holder of that pointer (e.g. cmd/botson-core's appBoot.Config) sees the
// change without needing to reload.
func TestUpdateMutatesSharedInstanceInPlace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetCacheForTest()

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	updated, err := Update(func(cfg *AppConfig) {
		cfg.RootAgent = "Changed Agent"
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if loaded != updated {
		t.Fatalf("Update returned a different pointer than Load; self-configuration would go unnoticed by existing holders")
	}
	if loaded.RootAgent != "Changed Agent" {
		t.Fatalf("original pointer from Load did not observe Update's change: got %q", loaded.RootAgent)
	}

	configPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath failed: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file directly: %v", err)
	}
	var onDisk AppConfig
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("failed to parse on-disk config: %v", err)
	}
	if onDisk.RootAgent != "Changed Agent" {
		t.Fatalf("Update did not persist to disk: got %q", onDisk.RootAgent)
	}
}

// resetCacheForTest clears the package-level cache between tests so each
// test's t.TempDir()-scoped HOME gets its own fresh Load, instead of
// silently reusing whatever an earlier test cached in-process.
func resetCacheForTest() {
	mu.Lock()
	defer mu.Unlock()
	cached = nil
}
