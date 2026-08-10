//go:build !windows

package authorization

import "os"

func replaceAuthorizationFile(source, destination string) error {
	return os.Rename(source, destination)
}
