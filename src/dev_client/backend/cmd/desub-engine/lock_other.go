//go:build !windows

package main

import "os"

// acquireLock on non-Windows builds is a best-effort existence lock; the
// dev client only ships for Windows, this just keeps cross-compiles working.
func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return func() {
		f.Close()
		os.Remove(path)
	}, nil
}
