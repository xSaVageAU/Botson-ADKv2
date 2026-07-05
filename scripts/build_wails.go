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
		fmt.Printf("Failed to get root directory: %v\n", err)
		os.Exit(1)
	}

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Printf("Failed to create bin directory: %v\n", err)
		os.Exit(1)
	}

	platforms := []struct {
		os   string
		arch string
		name string
	}{
		{"windows", "amd64", "adk-wails-windows-amd64.exe"},
	}

	for _, p := range platforms {
		fmt.Printf("Building for %s/%s...\n", p.os, p.arch)
		
		cmd := exec.Command("wails", "build", "-platform", p.os+"/"+p.arch, "-o", p.name)
		cmd.Dir = filepath.Join(rootDir, "cmd", "adk-wails")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Printf("Build failed for %s: %v\n", p.name, err)
			os.Exit(1)
		}

		source := filepath.Join(rootDir, "cmd", "adk-wails", "build", "bin", p.name)
		dest := filepath.Join(binDir, p.name)
		
		if err := os.Rename(source, dest); err != nil {
			fmt.Printf("Failed to move binary: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully built and moved: %s\n", dest)
	}
}
