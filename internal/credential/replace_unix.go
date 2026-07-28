//go:build !windows

package credential

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

func restrictFile(path string) error {
	return os.Chmod(path, 0o600)
}
