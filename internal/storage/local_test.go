package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorageRejectsTraversalKeys(t *testing.T) {
	root := t.TempDir()
	store := NewLocal(root, "http://example.test")

	if _, err := store.Put(context.Background(), "../escape.txt", strings.NewReader("bad")); err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file outside root, stat err: %v", err)
	}
}
