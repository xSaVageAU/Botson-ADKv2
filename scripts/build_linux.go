package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	outputPath := filepath.Join(outputDir, "botson-linux-amd64")
	fmt.Println("Building botson for linux/amd64...")

	cmd := exec.Command("go", "build", "-o", outputPath, ".")
	cmd.Dir = filepath.Join(rootDir, "cmd/botson-core")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Build failed: %v\n%s\n", err, string(output))
		os.Exit(1)
	}
	fmt.Println("Linux build successful.")
}
