package storage

import (
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

func TestSafeWorktreePath(t *testing.T) {
	root := filepath.Clean(paths.WorktreeDir())

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid two levels", filepath.Join(root, "myproject", "session-abc"), false},
		{"valid deeper", filepath.Join(root, "myproject", "session-abc", "sub"), false},
		{"root itself", root, true},
		{"bucket only", filepath.Join(root, "myproject"), true},
		{"traversal escape", filepath.Join(root, "myproject", "..", "..", "etc"), true},
		{"outside root", "/etc/passwd", true},
		{"relative path", "myproject/session-abc", true},
		{"sneaky prefix sibling", root + "-evil/x/y", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeWorktreePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("safeWorktreePath(%q) err = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
