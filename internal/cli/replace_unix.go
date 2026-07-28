//go:build !windows

package cli

import "os"

func replaceCLIFile(source, destination string) error {
	return os.Rename(source, destination)
}
