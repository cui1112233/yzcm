//go:build unix && !linux

package yzcmusage

import "os"

const receiptLegacyIdentityPlatform = "unix1"

func receiptObjectIdentity(*os.File) (receiptObjectID, error) {
	return receiptObjectID{}, errReceiptStableIdentityUnavailable
}
