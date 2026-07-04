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
		`build/bin/${BINNAME}.app`,
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
		`BINNAME="maddog-dev"`,
		"build/bin/maddog-dev.app",
	} {
		if strings.Contains(buildScript, forbidden) {
			t.Fatalf("desktop-build.sh still references %q", forbidden)
		}
	}

	runScript := readText(t, filepath.Join(root, "scripts", "build-package-run-maddog.ps1"))
	for _, want := range []string{
		`$DefaultLaunchProfileDir = Join-Path $RepoRoot ".maddog\desktop-run-profile"`,
		`$OutputName = "maddog"`,
	} {
		if !strings.Contains(runScript, want) {
			t.Fatalf("build-package-run-maddog.ps1 missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`$DefaultLaunchProfileDir = Join-Path $DesktopRoot "build\run-profile"`,
		`$OutputName = "maddog-dev"`,
	} {
		if strings.Contains(runScript, forbidden) {
			t.Fatalf("build-package-run-maddog.ps1 still references %q", forbidden)
		}
	}

	nsis := readText(t, filepath.Join(root, "desktop", "build", "windows", "installer", "project.nsi"))
	for _, want := range []string{
		`Maddog per-user NSIS installer`,
		`MADDOG_DEFAULT_INSTALLDIR`,
		`$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}`,
		`${INFO_PROJECTNAME}-${ARCH}-installer.exe`,
		`Keep AppData/WebView2 state intact`,
	} {
		if !strings.Contains(nsis, want) {
			t.Fatalf("project.nsi missing %q", want)
		}
	}
	if strings.Contains(nsis, `RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"`) {
		t.Fatal("project.nsi must not delete AppData/WebView2 state during uninstall/update")
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
