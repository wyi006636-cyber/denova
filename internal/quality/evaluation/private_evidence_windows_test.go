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

func TestPrivateEvidenceWritersUseOwnerOnlyWindowsACL(t *testing.T) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	harnessPath := filepath.Join(root, "private", "failures", "evidence.txt")
	if err := writePrivateHarnessOutput(harnessPath, []byte("private failure response")); err != nil {
		t.Fatal(err)
	}
	assertOwnerOnlyWindowsACL(t, user.User.Sid, harnessPath)

	runID := "run-private-review"
	sample := writeReadyBlindReviewFixture(t, root, runID)
	review := validReview(sample, "private-reviewer", "A")
	if err := SaveReview(root, runID, review); err != nil {
		t.Fatal(err)
	}
	reviews, err := loadReviews(root, runID)
	if err != nil || len(reviews) != 1 || reviews[0].ReviewID != review.ReviewID {
		t.Fatalf("accepted reviews=%#v err=%v", reviews, err)
	}
	reviewPath := filepath.Join(root, runID, "private", "reviews", review.ReviewID+".json")
	assertOwnerOnlyWindowsACL(t, user.User.Sid, reviewPath)
	assertOwnerOnlyWindowsACL(t, user.User.Sid, filepath.Join(root, runID, "private", "reviews", ".review.lock"))
}

func writeReadyBlindReviewFixture(t *testing.T, runRoot, runID string) BlindSample {
	t.Helper()
	sample := BlindSample{SampleID: "sample-private-review", Status: StatusReady}
	index := BlindIndex{
		Contract: "denova.quality-evaluation-blind-index", Version: "v1", RunID: runID,
		Status: StatusReady, Samples: []BlindSample{sample},
	}
	if err := writeJSONFile(filepath.Join(runRoot, runID, "blind", "package.json"), index); err != nil {
		t.Fatal(err)
	}
	return sample
}

func assertOwnerOnlyWindowsACL(t *testing.T, userSID *windows.SID, path string) {
	t.Helper()
	for _, target := range []string{filepath.Dir(path), path} {
		descriptor, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if err != nil {
			t.Fatalf("read ACL for %s: %v", target, err)
		}
		dacl, _, err := descriptor.DACL()
		owner, _, ownerErr := descriptor.Owner()
		if err != nil || ownerErr != nil || dacl == nil || dacl.AceCount != 1 || owner == nil || !owner.Equals(userSID) || !strings.Contains(descriptor.String(), "D:P") {
			t.Fatalf("%s does not have a protected owner-only DACL", target)
		}
		var ace *windows.ACCESS_ALLOWED_ACE
		err = windows.GetAce(dacl, 0, &ace)
		if err != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("%s does not have an allow ACE", target)
		}
		if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(userSID) {
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
