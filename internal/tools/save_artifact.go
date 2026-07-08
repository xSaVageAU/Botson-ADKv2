package tools

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/genai"
)

// SaveArtifactArgs defines the input arguments for the Save Artifact tool.
type SaveArtifactArgs struct {
	Name    string `json:"name" jsonschema:"The unique filename or identifier of the artifact (e.g., 'plan.md')."`
	Content string `json:"content" jsonschema:"The text content of the artifact to save."`
}

// SaveArtifact allows the agent to save or update a text artifact in the current session.
func SaveArtifact(ctx agent.Context, args SaveArtifactArgs) (string, error) {
	part := genai.NewPartFromText(args.Content)
	resp, err := ctx.Artifacts().Save(ctx, args.Name, part)
	if err != nil {
		return "", fmt.Errorf("failed to save artifact: %w", err)
	}
	return fmt.Sprintf("Successfully saved artifact %q as version %d", args.Name, resp.Version), nil
}
