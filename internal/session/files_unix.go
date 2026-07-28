//go:build !windows

package session

import "os"

func replaceSnapshot(source, destination string) error {
	return os.Rename(source, destination)
}

func restrictDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

func restrictFile(path string) error {
	return os.Chmod(path, 0o600)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
