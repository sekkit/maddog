package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePackagingRoutesEveryPlatformThroughGuard(t *testing.T) {
	root := filepath.Join("..", "..")
	checks := map[string][]string{
		filepath.Join(root, "scripts", "desktop-build.sh"): {
			"./cmd/maddog-guard",
			"Contents/MacOS/maddog-desktop",
			`"$staging/${APPNAME}.exe"`,
			`maddog-desktop`,
			`${APPNAME}-linux-${arch}-update.tar.gz`,
			`"$staging/maddog-guard"`,
		},
		filepath.Join(root, "desktop", "build", "windows", "installer", "project.nsi"): {
			`maddog-desktop.exe`,
			`maddog-guard.exe`,
		},
		filepath.Join(root, "desktop", "build", "linux", "nfpm.yaml"): {
			`build/guard/maddog-guard`,
			`/usr/lib/maddog/maddog-desktop`,
		},
	}
	for path, required := range checks {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range required {
			if !strings.Contains(string(raw), token) {
				t.Errorf("%s does not contain %q", path, token)
			}
		}
	}
}
