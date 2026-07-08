package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := WriteFile(nil, WriteFileArgs{
		FilePath: "nested/dir/hello.txt",
		Content:  "hello world",
	})
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if result.BytesWritten != len("hello world") {
		t.Fatalf("unexpected BytesWritten: %d", result.BytesWritten)
	}

	got, err := os.ReadFile(filepath.Join(root, "nested/dir/hello.txt"))
	if err != nil {
		t.Fatalf("failed to read back written file: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", got)
	}

	// Overwrite should replace, not append.
	if _, err := WriteFile(nil, WriteFileArgs{FilePath: "nested/dir/hello.txt", Content: "bye"}); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "nested/dir/hello.txt"))
	if string(got) != "bye" {
		t.Fatalf("overwrite did not replace content, got %q", got)
	}

	// Escaping the workspace must be rejected.
	if _, err := WriteFile(nil, WriteFileArgs{FilePath: "../escape.txt", Content: "x"}); err == nil {
		t.Fatal("expected an error writing outside the workspace, got none")
	}
}
