//go:build windows

package cli

import (
	"syscall"
	"unsafe"
)

const (
	cliMoveFileReplaceExisting = 0x1
	cliMoveFileWriteThrough    = 0x8
)

var cliMoveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceCLIFile(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := cliMoveFileEx.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(destinationPointer)),
		cliMoveFileReplaceExisting|cliMoveFileWriteThrough,
	)
	if result == 0 {
		return callErr
	}
	return nil
}
