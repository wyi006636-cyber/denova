//go:build windows

package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWritePrivateHarnessOutputUsesOwnerOnlyWindowsACL(t *testing.T) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private", "failures", "evidence.txt")
	if err := writePrivateHarnessOutput(path, []byte("private failure response")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Dir(path), path} {
		descriptor, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if err != nil {
			t.Fatalf("read ACL for %s: %v", target, err)
		}
		dacl, _, err := descriptor.DACL()
		owner, _, ownerErr := descriptor.Owner()
		if err != nil || ownerErr != nil || dacl == nil || dacl.AceCount != 1 || owner == nil || !owner.Equals(user.User.Sid) || !strings.Contains(descriptor.String(), "D:P") {
			t.Fatalf("%s does not have a protected owner-only DACL", target)
		}
		var ace *windows.ACCESS_ALLOWED_ACE
		err = windows.GetAce(dacl, 0, &ace)
		if err != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("%s does not have an allow ACE", target)
		}
		if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
			t.Fatalf("%s does not grant its only ACE to the current owner", target)
		}
		fullFileAccess := windows.ACCESS_MASK(windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1FF)
		if ace.Mask != windows.GENERIC_ALL && ace.Mask != fullFileAccess {
			t.Fatalf("%s ACE mask=%#x does not grant full file access", target, ace.Mask)
		}
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
}
