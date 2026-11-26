//go:build linux || darwin

package productionmedia

import (
	"path/filepath"
	"testing"
)

func TestDiskUsagePercentOnExistingAndMissingFilesystem(t *testing.T) {
	directory := t.TempDir()
	used, err := DiskUsagePercent(directory)
	if err != nil || used < 0 || used > 100 {
		t.Fatalf("filesystem usage=%v error=%v", used, err)
	}
	if _, err := DiskUsagePercent(filepath.Join(directory, "missing", "child")); err == nil {
		t.Fatal("a nonexistent filesystem path must not be accepted as free disk space")
	}
}
