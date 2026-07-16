//go:build windows

package fileutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivatePathsUseProtectedWindowsDACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "artifact")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ProtectPrivateFile(path); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{dir, path} {
		sd, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		sddl := sd.String()
		for _, forbidden := range []string{";;;WD)", ";;;BU)", ";;;AU)"} {
			if strings.Contains(sddl, forbidden) {
				t.Fatalf("DACL for %s grants broad trustee %s: %s", target, forbidden, sddl)
			}
		}
	}
}
