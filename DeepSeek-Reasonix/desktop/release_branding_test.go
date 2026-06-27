package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopReleaseScriptsUseMaddogArtifacts(t *testing.T) {
	root := repoRootForDesktopTest(t)
	buildScript := readText(t, filepath.Join(root, "scripts", "desktop-build.sh"))
	for _, want := range []string{
		`APPNAME="Maddog"`,
		`BINNAME="maddog"`,
		`${APPNAME}-windows-${arch}-installer.exe`,
		`${APPNAME}-windows-${arch}.zip`,
		`${APPNAME}-linux-${arch}.tar.gz`,
		`${APPNAME}-linux-${arch}.deb`,
	} {
		if !strings.Contains(buildScript, want) {
			t.Fatalf("desktop-build.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Reasonix-windows",
		"Reasonix-linux",
		"reasonix-dev.exe",
	} {
		if strings.Contains(buildScript, forbidden) {
			t.Fatalf("desktop-build.sh still references %q", forbidden)
		}
	}

	nsis := readText(t, filepath.Join(root, "desktop", "build", "windows", "installer", "project.nsi"))
	for _, want := range []string{
		`Maddog per-user NSIS installer`,
		`MADDOG_DEFAULT_INSTALLDIR`,
		`$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}`,
		`${INFO_PROJECTNAME}-${ARCH}-installer.exe`,
	} {
		if !strings.Contains(nsis, want) {
			t.Fatalf("project.nsi missing %q", want)
		}
	}
	if strings.Contains(nsis, `Reasonix`) {
		t.Fatalf("project.nsi should not contain Reasonix branding")
	}
}

func repoRootForDesktopTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if exists(filepath.Join(dir, "go.mod")) && exists(filepath.Join(dir, "scripts", "desktop-build.sh")) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
