// Package datalock excludes application and offline migration database users.
package datalock

import (
	"fmt"
	"os"
	"syscall"
)

// Acquire locks the directory inode, shared with the offline migration CLI.
// Closing the returned descriptor releases the lock, including after a crash.
func Acquire(folder string) (*os.File, error) {
	file, err := os.Open(folder)
	if err != nil {
		return nil, fmt.Errorf("opening data directory: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("data directory is busy: %w", err)
	}
	return file, nil
}
