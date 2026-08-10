//go:build !windows

package tasks

import "os"

func replaceTaskQueueFile(source, destination string) error {
	return os.Rename(source, destination)
}
