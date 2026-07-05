package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

func main() {
	binaryName := "botsonv2"
	outputDir := "../../bin"

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

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(target struct {
			goos   string
			goarch string
			suffix string
		}) {
			defer wg.Done()
			outputPath := fmt.Sprintf("%s/%s-%s-%s%s", outputDir, binaryName, target.goos, target.goarch, target.suffix)
			fmt.Printf("Building for %s/%s...\n", target.goos, target.goarch)

		cmd := exec.Command("go", "build", "-o", outputPath, ".")
		cmd.Dir = "cmd/adk-web"
		cmd.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch)


			
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Build failed for %s/%s: %v\n%s\n", target.goos, target.goarch, err, string(output))
				os.Exit(1)
			}
			fmt.Printf("Build successful for %s/%s.\n", target.goos, target.goarch)
		}(t)
	}
	wg.Wait()
	fmt.Println("All builds successful.")
}
