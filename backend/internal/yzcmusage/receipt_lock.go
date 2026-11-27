package yzcmusage

import (
	"context"
	"os"
	"time"
)

func openReceiptLock(ctx context.Context, path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for ctx.Err() == nil {
		locked, err := tryReceiptLock(f)
		if locked {
			return f, nil
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	f.Close()
	return nil, ctx.Err()
}
