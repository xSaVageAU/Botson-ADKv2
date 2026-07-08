package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspacePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	if err := os.WriteFile(filepath.Join(root, "in-root.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// A sibling directory that merely shares root's string prefix must
	// NOT be treated as inside the workspace -- this is the boundary bug
	// resolveWorkspacePath exists to fix.
	siblingRoot := root + "-evil"
	if err := os.MkdirAll(siblingRoot, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	siblingFile := filepath.Join(siblingRoot, "secret.txt")
	if err := os.WriteFile(siblingFile, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative path inside root", "in-root.txt", false},
		{"nested relative path", "sub/dir/file.txt", false},
		{"absolute path inside root", filepath.Join(root, "in-root.txt"), false},
		{"traversal outside root", "../escape.txt", true},
		{"sibling dir sharing string prefix", siblingFile, true},
		{"the .env file", ".env", true},
		{"the .env file, different case", ".ENV", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolveWorkspacePath(c.path)
			if c.wantErr && err == nil {
				t.Fatalf("expected an error for %q, got none", c.path)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.path, err)
			}
		})
	}
}
