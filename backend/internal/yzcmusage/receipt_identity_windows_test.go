package yzcmusage

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const receiptLegacyFixturePlatform = "win1"

func legacyReceiptFixtureFileID(t *testing.T, path string) (uint64, [16]byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var id windowsReceiptFileID
	if err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))); err != nil {
		t.Fatal(err)
	}
	return id.volume, id.file
}
