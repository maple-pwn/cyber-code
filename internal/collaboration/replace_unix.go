//go:build !windows

package collaboration

import "os"

func replaceBoardFile(source, destination string) error { return os.Rename(source, destination) }
