package yzcmusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errReceiptStableIdentityUnavailable = errors.New("usage receipt filesystem does not provide a stable object identity")

const receiptIdentityV2Prefix = "fs2:"

// The previous format is used only to verify an existing v1 MySQL binding.
// Its layout matches Windows FILE_ID_INFO; Linux stored its inode in file[8:].
type receiptLegacyObjectID struct {
	volume uint64
	file   [16]byte
}

type receiptObjectID struct {
	receiptLegacyObjectID
	proof            [sha256.Size]byte
	birth            time.Time
	birthUncertainty time.Duration
}

func (id receiptObjectID) same(other receiptObjectID) bool {
	return id.proof != ([sha256.Size]byte{}) && id.proof == other.proof
}

type receiptFilesystemObjects struct {
	parent, directory, marker, anchor receiptObjectID
}

func (objects receiptFilesystemObjects) encode() string {
	var proofs [4 * sha256.Size]byte
	copy(proofs[0*sha256.Size:], objects.parent.proof[:])
	copy(proofs[1*sha256.Size:], objects.directory.proof[:])
	copy(proofs[2*sha256.Size:], objects.marker.proof[:])
	copy(proofs[3*sha256.Size:], objects.anchor.proof[:])
	digest := sha256.Sum256(proofs[:])
	return receiptIdentityV2Prefix + hex.EncodeToString(digest[:])
}

func (objects receiptFilesystemObjects) same(other receiptFilesystemObjects) bool {
	return objects.parent.same(other.parent) && objects.directory.same(other.directory) &&
		objects.marker.same(other.marker) && objects.anchor.same(other.anchor)
}

func (objects receiptFilesystemObjects) legacyEncoding() string {
	var encoded strings.Builder
	encoded.WriteString(receiptLegacyIdentityPlatform)
	for _, id := range []receiptObjectID{objects.parent, objects.directory, objects.marker, objects.anchor} {
		if id.volume == 0 || id.file == ([16]byte{}) {
			return ""
		}
		fmt.Fprintf(&encoded, ":%016x%s", id.volume, hex.EncodeToString(id.file[:]))
	}
	return encoded.String()
}

func openReceiptObject(path string, directory, writable bool) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return nil, errReceiptDirectoryChanged
	}
	var f *os.File
	switch {
	case directory && writable:
		f, err = openReceiptDirectoryForSync(path)
	case writable:
		f, err = os.OpenFile(path, os.O_RDWR, 0)
	default:
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		f.Close()
		return nil, errors.Join(errReceiptDirectoryChanged, err)
	}
	return f, nil
}

func readReceiptObject(path string, directory bool, marker string) (receiptObjectID, error) {
	f, err := openReceiptObject(path, directory, false)
	if err != nil {
		return receiptObjectID{}, err
	}
	defer f.Close()
	if !directory {
		id, err := readReceiptMarker(f)
		if err != nil || id != marker {
			return receiptObjectID{}, errors.Join(errReceiptDirectoryChanged, err)
		}
	}
	return receiptObjectIdentity(f)
}

func (b receiptBinding) filesystemObjects() (receiptFilesystemObjects, error) {
	var objects receiptFilesystemObjects
	for _, item := range []struct {
		path      string
		directory bool
		object    *receiptObjectID
	}{
		{filepath.Dir(b.dir), true, &objects.parent},
		{b.dir, true, &objects.directory},
		{filepath.Join(b.dir, receiptDirectoryIDFile), false, &objects.marker},
		{b.anchor, false, &objects.anchor},
	} {
		id, err := readReceiptObject(item.path, item.directory, b.id)
		if err != nil {
			return receiptFilesystemObjects{}, err
		}
		*item.object = id
	}
	return objects, nil
}

func syncReceiptDirectory(path string) error {
	f, err := openReceiptObject(path, true, true)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func (b receiptBinding) syncPendingFilesystem(ctx context.Context, parentLevels int, syncFile func(*os.File) error) (string, error) {
	objects, err := b.filesystemObjects()
	if err != nil {
		return "", err
	}
	if syncFile == nil {
		syncFile = (*os.File).Sync
	}
	syncExpected := func(path string, directory bool, expected receiptObjectID) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := openReceiptObject(path, directory, true)
		if err != nil {
			return err
		}
		defer f.Close()
		if expected != (receiptObjectID{}) {
			actual, err := receiptObjectIdentity(f)
			if err != nil || !actual.same(expected) {
				return errors.Join(errReceiptDirectoryChanged, err)
			}
		}
		if !directory {
			id, err := readReceiptMarker(f)
			if err != nil || id != b.id {
				return errors.Join(errReceiptDirectoryChanged, err)
			}
		}
		return syncFile(f)
	}
	for _, item := range []struct {
		path      string
		directory bool
		id        receiptObjectID
	}{
		{filepath.Join(b.dir, receiptDirectoryIDFile), false, objects.marker},
		{b.anchor, false, objects.anchor},
		{b.dir, true, objects.directory},
	} {
		if err := syncExpected(item.path, item.directory, item.id); err != nil {
			return "", err
		}
	}
	// The original ancestor depth is saved in the pending row so a restarted
	// attempt also flushes directories that an earlier attempt already made.
	for level, parent := 0, filepath.Dir(b.dir); level < parentLevels; level, parent = level+1, filepath.Dir(parent) {
		var id receiptObjectID
		if level == 0 {
			id = objects.parent
		}
		if err := syncExpected(parent, true, id); err != nil {
			return "", err
		}
	}
	b.objects = objects
	if err := b.verify(); err != nil {
		return "", err
	}
	return objects.encode(), nil
}
