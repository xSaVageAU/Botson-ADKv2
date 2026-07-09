package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFile(t *testing.T) {
	root := t.TempDir()
	old := WorkspaceRoot
	WorkspaceRoot = root
	t.Cleanup(func() { WorkspaceRoot = old })

	writeAndRead := func(t *testing.T, ctx *fakeContext, path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0644); err != nil {
			t.Fatalf("setup write failed: %v", err)
		}
		if _, err := ReadFile(ctx, ReadFileArgs{FilePath: path}); err != nil {
			t.Fatalf("setup read failed: %v", err)
		}
	}

	t.Run("unique match is replaced", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "unique.txt", "hello world\ngoodbye world\n")

		result, err := EditFile(ctx, EditFileArgs{
			FilePath:  "unique.txt",
			OldString: "hello world",
			NewString: "hi world",
		})
		if err != nil {
			t.Fatalf("EditFile failed: %v", err)
		}
		if result.OccurrencesReplaced != 1 {
			t.Fatalf("expected 1 occurrence replaced, got %d", result.OccurrencesReplaced)
		}

		got, _ := os.ReadFile(filepath.Join(root, "unique.txt"))
		want := "hi world\ngoodbye world\n"
		if string(got) != want {
			t.Fatalf("unexpected content: %q, want %q", got, want)
		}
	})

	t.Run("replaceAll replaces every occurrence", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "repeat.txt", "foo foo foo\n")

		result, err := EditFile(ctx, EditFileArgs{
			FilePath:   "repeat.txt",
			OldString:  "foo",
			NewString:  "bar",
			ReplaceAll: true,
		})
		if err != nil {
			t.Fatalf("EditFile failed: %v", err)
		}
		if result.OccurrencesReplaced != 3 {
			t.Fatalf("expected 3 occurrences replaced, got %d", result.OccurrencesReplaced)
		}

		got, _ := os.ReadFile(filepath.Join(root, "repeat.txt"))
		if string(got) != "bar bar bar\n" {
			t.Fatalf("unexpected content: %q", got)
		}
	})

	t.Run("not found is a clear error", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "nomatch.txt", "hello world\n")

		_, err := EditFile(ctx, EditFileArgs{FilePath: "nomatch.txt", OldString: "not present", NewString: "x"})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected a 'not found' error, got: %v", err)
		}
	})

	t.Run("ambiguous match without replaceAll is rejected", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "ambiguous.txt", "foo foo\n")

		_, err := EditFile(ctx, EditFileArgs{FilePath: "ambiguous.txt", OldString: "foo", NewString: "bar"})
		if err == nil || !strings.Contains(err.Error(), "expected exactly 1") {
			t.Fatalf("expected an 'expected exactly 1' error, got: %v", err)
		}
	})

	t.Run("editing without a prior read is rejected", func(t *testing.T) {
		ctx := newFakeContext()
		if err := os.WriteFile(filepath.Join(root, "unread.txt"), []byte("hello\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		_, err := EditFile(ctx, EditFileArgs{FilePath: "unread.txt", OldString: "hello", NewString: "hi"})
		if err == nil || !strings.Contains(err.Error(), "must read") {
			t.Fatalf("expected a 'must read' guard error, got: %v", err)
		}
	})

	t.Run("editing a nonexistent file is rejected", func(t *testing.T) {
		ctx := newFakeContext()
		_, err := EditFile(ctx, EditFileArgs{FilePath: "ghost.txt", OldString: "a", NewString: "b"})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("expected a 'does not exist' error, got: %v", err)
		}
	})

	t.Run("empty oldString is rejected", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "empty.txt", "hello\n")

		_, err := EditFile(ctx, EditFileArgs{FilePath: "empty.txt", OldString: "", NewString: "x"})
		if err == nil {
			t.Fatal("expected an error for empty oldString, got none")
		}
	})

	t.Run("identical oldString and newString is rejected", func(t *testing.T) {
		ctx := newFakeContext()
		writeAndRead(t, ctx, "noop.txt", "hello\n")

		_, err := EditFile(ctx, EditFileArgs{FilePath: "noop.txt", OldString: "hello", NewString: "hello"})
		if err == nil {
			t.Fatal("expected an error for a no-op edit, got none")
		}
	})
}
