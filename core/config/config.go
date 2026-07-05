package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppConfig holds the application configuration.
type AppConfig struct {
	ModelName      string
	GeminiAPIKey   string
}

// Load loads configuration from the .env file in the executable directory.
func Load() (*AppConfig, error) {
	var envData []byte
	var err error

	// 1. Try reading .env in current working directory (e.g., go run during development)
	if envData, err = os.ReadFile(".env"); err != nil {
		// 2. Fall back to executable directory (e.g., compiled production binary)
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exePath)
			if envData, err = os.ReadFile(filepath.Join(exeDir, ".env")); err == nil {
				os.Chdir(exeDir)
			}
		}
	}

	// Parse .env file if successfully loaded
	if envData != nil {
		for _, line := range strings.Split(string(envData), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY not found in .env or environment")
	}

	return &AppConfig{
		ModelName:    "gemini-3.1-flash-lite",
		GeminiAPIKey: apiKey,
	}, nil
}
