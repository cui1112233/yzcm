//go:build unix

package yzcmusage

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func tryReceiptLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

const receiptIdentityPlatform = "unix1"

func receiptObjectIdentity(f *os.File) (receiptObjectID, error) {
	info, err := f.Stat()
	if err != nil {
		return receiptObjectID{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return receiptObjectID{}, errReceiptStableIdentityUnavailable
	}
	id := receiptObjectID{volume: uint64(stat.Dev)}
	binary.BigEndian.PutUint64(id.file[8:], uint64(stat.Ino))
	return id.checked()
}

func openReceiptDirectoryForSync(dir string) (*os.File, error) { return os.Open(dir) }
