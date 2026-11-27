package yzcmusage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func copyReceiptFixtureTree(t *testing.T, source, target string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if item.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		if !item.Type().IsRegular() {
			return errors.New("unexpected non-regular file in receipt fixture")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0600)
	}); err != nil {
		t.Fatal(err)
	}
}

func otherReceiptTestVolume(t *testing.T) string {
	t.Helper()
	base := os.Getenv("QIANTIE_RECEIPT_OTHER_VOLUME_TEST_DIR")
	if base == "" && runtime.GOOS == "linux" {
		if info, err := os.Stat("/dev/shm"); err == nil && info.IsDir() {
			base = "/dev/shm"
		}
	}
	if base == "" {
		t.Log("cross-volume fixture not configured")
		return ""
	}
	base, err := filepath.Abs(base)
	if err == nil {
		base, err = filepath.EvalSymlinks(base)
	}
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "qiantie-receipt-volume-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// This is the exact new directory created for this test, on an explicit volume.
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || resolved != dir || filepath.Dir(resolved) != base {
			t.Errorf("refusing cleanup of unexpected fixture directory: %q %v", resolved, err)
			return
		}
		if err := os.RemoveAll(resolved); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func assertOtherReceiptVolume(t *testing.T, source, target string) {
	t.Helper()
	from, err := readReceiptObject(source, true, "")
	if err != nil {
		t.Fatal(err)
	}
	to, err := readReceiptObject(target, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if from.volume == to.volume {
		t.Fatal("cross-volume fixture uses the same volume or device")
	}
	t.Log("verified distinct native volume or device IDs")
}

func TestReceiptFilesystemIdentitySurvivesRenameAndRejectsCopies(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "members")
	dir := filepath.Join(parent, "usage-receipts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tracker := &Tracker{ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
	original := *tracker.receiptBound
	moved := filepath.Join(root, "renamed-members")
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	b := original
	b.dir, b.anchor = filepath.Join(moved, "usage-receipts"), filepath.Join(moved, filepath.Base(original.anchor))
	if err := b.verifyVisible(); err != nil {
		t.Fatalf("rename changed identity: %v", err)
	}
	copyReceiptFixtureTree(t, moved, parent)
	copyInfo, err := visibleReceiptDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	b.dir, b.anchor, b.info = dir, original.anchor, copyInfo
	if err := b.verifyVisible(); !errors.Is(err, errReceiptDirectoryChanged) {
		t.Fatalf("copied objects matched the old authority: %v", err)
	}
	if alt := otherReceiptTestVolume(t); alt != "" {
		crossVolume := filepath.Join(alt, "members")
		copyReceiptFixtureTree(t, moved, crossVolume)
		assertOtherReceiptVolume(t, moved, crossVolume)
		b.dir = filepath.Join(crossVolume, "usage-receipts")
		b.anchor = filepath.Join(crossVolume, filepath.Base(original.anchor))
		b.info, err = visibleReceiptDirectory(b.dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.verifyVisible(); !errors.Is(err, errReceiptDirectoryChanged) {
			t.Fatalf("cross-volume copy matched the old authority: %v", err)
		}
	}
}
