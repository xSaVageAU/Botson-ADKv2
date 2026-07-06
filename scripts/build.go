package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

func main() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Failed to get working directory: %v\n", err)
		os.Exit(1)
	}

	outputDir := filepath.Join(rootDir, "bin")
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, 0755)
	}

	targets := []struct {
		goos   string
		goarch string
		suffix string
	}{
		{"windows", "amd64", ".exe"},
		{"linux", "amd64", ""},
	}

	apps := []struct {
		name string
		dir  string
	}{
		{"botsonv2-adk", "cmd/botson-adk"},
		{"botsonv2-builder", "cmd/agent-builder"},
		{"botsonv2-prod", "cmd/botson-prod"},
		{"botsonv2-discord", "cmd/botson-discord"},
	}

	var wg sync.WaitGroup
	for _, t := range targets {
		for _, app := range apps {
			wg.Add(1)
			go func(target struct {
				goos   string
				goarch string
				suffix string
			}, app struct {
				name string
				dir  string
			}) {
				defer wg.Done()
				outputPath := filepath.Join(outputDir, fmt.Sprintf("%s-%s-%s%s", app.name, target.goos, target.goarch, target.suffix))
				fmt.Printf("Building %s for %s/%s...\n", app.name, target.goos, target.goarch)

				cmd := exec.Command("go", "build", "-o", outputPath, ".")
				cmd.Dir = filepath.Join(rootDir, app.dir)
				cmd.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch)

				output, err := cmd.CombinedOutput()
				if err != nil {
					fmt.Printf("Build failed for %s (%s/%s): %v\n%s\n", app.name, target.goos, target.goarch, err, string(output))
					os.Exit(1)
				}
				fmt.Printf("Build successful for %s (%s/%s).\n", app.name, target.goos, target.goarch)
			}(t, app)
		}
	}
	wg.Wait()
	fmt.Println("All builds successful.")
}
