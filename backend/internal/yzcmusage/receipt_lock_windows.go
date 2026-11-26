package yzcmusage

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func tryReceiptLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

const receiptIdentityPlatform = "win1"

func receiptObjectIdentity(f *os.File) (receiptObjectID, error) {
	handle := windows.Handle(f.Fd())
	var flags uint32
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return receiptObjectID{}, err
	}
	if flags&windows.FILE_SUPPORTS_OPEN_BY_FILE_ID == 0 {
		return receiptObjectID{}, errReceiptStableIdentityUnavailable
	}
	// FILE_ID_INFO contains a 64-bit volume serial and a 128-bit file ID.
	var id receiptObjectID
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))); err != nil {
		return receiptObjectID{}, err
	}
	return id.checked()
}

func openReceiptDirectoryForSync(dir string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), dir), nil
}
