//go:build windows

package credential

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertPrivateStorePermissions(t *testing.T, directory, path string) {
	t.Helper()
	for _, target := range []string{directory, path} {
		descriptor, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		dacl, _, err := descriptor.DACL()
		if err != nil || dacl == nil || dacl.AceCount != 1 {
			t.Fatalf("private path %q DACL=%v err=%v", target, dacl, err)
		}
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, 0, &ace); err != nil {
			t.Fatal(err)
		}
		currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			t.Fatal(err)
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !aceSID.Equals(currentUser.User.Sid) || ace.Mask != privateFileFullControl {
			t.Fatalf("private path %q does not grant only current user full control", target)
		}
	}
}
