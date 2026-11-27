package yzcmusage

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const receiptLegacyIdentityPlatform = "unix1"

// AT_HANDLE_FID (Linux 6.5+) asks for object identity without requiring a
// reopenable handle. This also supports overlay filesystems without NFS export.
const receiptHandleFID = 0x200

var receiptLinuxBirthUncertainty = sync.OnceValue(func() time.Duration {
	var resolution unix.Timespec
	if err := unix.ClockGetres(unix.CLOCK_REALTIME_COARSE, &resolution); err != nil {
		return 0
	}
	return time.Duration(resolution.Sec)*time.Second + time.Duration(resolution.Nsec)
})

func receiptObjectIdentity(f *os.File) (receiptObjectID, error) {
	fd := int(f.Fd())
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil {
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	fsType := uint32(fs.Type)
	switch fsType {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, unix.TMPFS_MAGIC:
	default:
		return receiptObjectID{}, fmt.Errorf("%w: Linux filesystem type %#x", errReceiptStableIdentityUnavailable, fsType)
	}
	if fs.Fsid.Val == ([2]int32{}) {
		return receiptObjectID{}, fmt.Errorf("%w: Linux filesystem ID is empty", errReceiptStableIdentityUnavailable)
	}
	fileHandle, _, err := unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH|receiptHandleFID)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		fileHandle, _, err = unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH)
	}
	if err != nil {
		// A filesystem creation timestamp can be shared by two creations in the
		// same kernel clock tick. Without a generation-bearing handle we cannot
		// establish a unique lifetime from a timestamp and inode alone.
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	if fileHandle.Type() <= 0 || len(fileHandle.Bytes()) == 0 {
		return receiptObjectID{}, fmt.Errorf("%w: Linux file handle is empty", errReceiptStableIdentityUnavailable)
	}
	var id receiptObjectID
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return receiptObjectID{}, errors.Join(errReceiptStableIdentityUnavailable, err)
	}
	id.volume = uint64(stat.Dev)
	binary.BigEndian.PutUint64(id.file[8:], uint64(stat.Ino))
	var header [24]byte
	copy(header[:4], "lnx2")
	binary.BigEndian.PutUint64(header[4:12], uint64(fsType))
	binary.BigEndian.PutUint32(header[12:16], uint32(fs.Fsid.Val[0]))
	binary.BigEndian.PutUint32(header[16:20], uint32(fs.Fsid.Val[1]))
	binary.BigEndian.PutUint32(header[20:24], uint32(fileHandle.Type()))
	id.proof = sha256.Sum256(append(header[:], fileHandle.Bytes()...))
	// Birth time is additional evidence only for upgrading an active v1 row.
	// An already-bound v2 handle does not depend on statx support.
	var extra unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_BTIME, &extra); err == nil && extra.Mask&unix.STATX_BTIME != 0 && extra.Btime.Sec > 0 && extra.Btime.Nsec < 1_000_000_000 {
		id.birth = time.Unix(extra.Btime.Sec, int64(extra.Btime.Nsec)).UTC()
		id.birthUncertainty = receiptLinuxBirthUncertainty()
	}
	return id, nil
}
