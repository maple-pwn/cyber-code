//go:build !windows

package runtimeapi

import "os"

func replaceLocalStateFile(source, destination string) error {
	return os.Rename(source, destination)
}
