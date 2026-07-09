package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadFile(t *testing.T) {
	root := t.TempDir()
	old := WorkspaceRoot
	WorkspaceRoot = root
	t.Cleanup(func() { WorkspaceRoot = old })

	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, "ten.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("default reads the whole file, cat -n formatted", func(t *testing.T) {
		result, err := ReadFile(nil, ReadFileArgs{FilePath: "ten.txt"})
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if result.TotalLines != 10 {
			t.Fatalf("expected TotalLines 10, got %d", result.TotalLines)
		}
		if result.StartLine != 1 || result.EndLine != 10 {
			t.Fatalf("expected StartLine 1, EndLine 10, got %d, %d", result.StartLine, result.EndLine)
		}
		if result.Truncated {
			t.Fatal("expected Truncated false when the whole file fits")
		}
		wantFirstLine := "     1\tline 1"
		if !strings.HasPrefix(result.Content, wantFirstLine) {
			t.Fatalf("expected content to start with %q, got %q", wantFirstLine, result.Content)
		}
	})

	t.Run("offset/limit paginate", func(t *testing.T) {
		result, err := ReadFile(nil, ReadFileArgs{FilePath: "ten.txt", Offset: 3, Limit: 2})
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if result.StartLine != 3 || result.EndLine != 4 {
			t.Fatalf("expected StartLine 3, EndLine 4, got %d, %d", result.StartLine, result.EndLine)
		}
		if !result.Truncated {
			t.Fatal("expected Truncated true when more lines remain")
		}
		if !strings.Contains(result.Content, "line 3") || !strings.Contains(result.Content, "line 4") {
			t.Fatalf("expected content to contain lines 3-4, got %q", result.Content)
		}
		if strings.Contains(result.Content, "line 5") {
			t.Fatalf("expected content to stop at line 4, got %q", result.Content)
		}
	})

	t.Run("a read unlocks a subsequent write on the same context", func(t *testing.T) {
		ctx := newFakeContext()
		if _, err := ReadFile(ctx, ReadFileArgs{FilePath: "ten.txt", Offset: 5, Limit: 1}); err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if _, err := WriteFile(ctx, WriteFileArgs{FilePath: "ten.txt", Content: "replaced\n"}); err != nil {
			t.Fatalf("expected write to succeed after a partial read, got: %v", err)
		}
	})
}
