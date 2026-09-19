//go:build !windows

package cardkey

import "os"

func atomicReplace(source, target string) error { return os.Rename(source, target) }
