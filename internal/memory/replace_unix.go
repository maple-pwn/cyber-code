//go:build !windows

package memory

import "os"

func replaceMemoryFile(source, destination string) error { return os.Rename(source, destination) }
