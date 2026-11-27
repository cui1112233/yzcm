//go:build unix && !linux

package yzcmusage

import "os"

func receiptObjectIdentity(*os.File) (receiptObjectID, error) {
	return receiptObjectID{}, errReceiptStableIdentityUnavailable
}
