package yzcmusage

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const receiptLegacyIdentityPlatform = "win1"

var receiptWindowsBirthUncertainty = sync.OnceValue(func() time.Duration {
	query := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemTimeAdjustment")
	if err := query.Find(); err != nil {
		return 0
	}
	var adjustment, increment, disabled uint32
	result, _, _ := query.Call(uintptr(unsafe.Pointer(&adjustment)), uintptr(unsafe.Pointer(&increment)), uintptr(unsafe.Pointer(&disabled)))
	if result == 0 {
		return 0
	}
	return time.Duration(increment) * (100 * time.Nanosecond)
})

func receiptObjectIdentity(f *os.File) (receiptObjectID, error) {
	handle := windows.Handle(f.Fd())
	var flags uint32
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	if flags&windows.FILE_SUPPORTS_OPEN_BY_FILE_ID == 0 {
		return receiptObjectID{}, errReceiptStableIdentityUnavailable
	}
	var native receiptLegacyObjectID
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&native)), uint32(unsafe.Sizeof(native))); err != nil {
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	created := uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime)
	if native.volume == 0 || native.file == ([16]byte{}) || created == 0 || info.CreationTime.Nanoseconds() <= 0 {
		return receiptObjectID{}, errReceiptStableIdentityUnavailable
	}
	var raw [36]byte
	copy(raw[:4], "win2")
	binary.BigEndian.PutUint64(raw[4:12], native.volume)
	copy(raw[12:28], native.file[:])
	binary.BigEndian.PutUint64(raw[28:36], created)
	return receiptObjectID{
		receiptLegacyObjectID: native,
		proof:                 sha256.Sum256(raw[:]),
		birth:                 time.Unix(0, info.CreationTime.Nanoseconds()).UTC(),
		birthUncertainty:      receiptWindowsBirthUncertainty(),
	}, nil
}
