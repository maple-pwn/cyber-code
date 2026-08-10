//go:build !windows

package marketplace

import "os"

func replaceMarketplaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
