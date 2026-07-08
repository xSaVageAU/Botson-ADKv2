package tools

import (
	"os"
	"path/filepath"
	"strings"
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

// TestWriteFileReadBeforeWriteGuard exercises the guard with a real
// (fake) context -- a nil ctx, as used above, always bypasses it, so
// these cases need something that actually backs State().
func TestWriteFileReadBeforeWriteGuard(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	ctx := newFakeContext()

	// A brand-new file needs no prior read.
	if _, err := WriteFile(ctx, WriteFileArgs{FilePath: "new.txt", Content: "v1"}); err != nil {
		t.Fatalf("writing a new file should not require a prior read: %v", err)
	}

	// A file that already exists on disk but was never read via ReadFile
	// in this session (simulated here with a fresh context and a file
	// written directly to disk, bypassing WriteFile) must be rejected.
	ctx2 := newFakeContext()
	if err := os.WriteFile(filepath.Join(root, "external.txt"), []byte("v1"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := WriteFile(ctx2, WriteFileArgs{FilePath: "external.txt", Content: "v2"}); err == nil || !strings.Contains(err.Error(), "must read") {
		t.Fatalf("expected a 'must read' guard error overwriting an existing file with no prior read, got: %v", err)
	}

	// Reading it first, then writing, succeeds.
	if _, err := ReadFile(ctx2, ReadFileArgs{FilePath: "external.txt"}); err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if _, err := WriteFile(ctx2, WriteFileArgs{FilePath: "external.txt", Content: "v2"}); err != nil {
		t.Fatalf("write after read should succeed: %v", err)
	}
}
