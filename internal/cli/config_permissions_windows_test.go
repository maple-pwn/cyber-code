//go:build windows

package cli

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	configpkg "cyber-code/internal/config"
)

func TestSaveCommandConfigProtectsWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	configured := configpkg.Default()
	if err := saveCommandConfig(path, &configured); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("private config DACL ACE count = %v", dacl)
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
	if !aceSID.Equals(currentUser.User.Sid) || ace.Mask != windows.GENERIC_ALL {
		t.Fatalf("private config ACE does not grant only current user full access")
	}
}
