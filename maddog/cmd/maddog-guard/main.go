// Command maddog-guard is the release entry point for the Maddog desktop.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"maddog/internal/config"
	"maddog/internal/processlock"
	"maddog/internal/repair"
)

var desktopName = "maddog-desktop"

func main() { os.Exit(run()) }

func run() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "maddog guard:", err)
		return 1
	}
	lease, primary, err := processlock.Try(filepath.Join(config.MemoryUserDir(), "desktop-runtime.lock"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "maddog guard:", err)
		return 1
	}
	if primary {
		defer lease.Release()
	}
	guard := repair.NewStartupGuard(filepath.Join(config.MemoryUserDir(), "startup-probation.json"), 3, 2*time.Minute)
	for {
		if primary {
			safe, err := guard.Begin()
			if err != nil || safe {
				fmt.Fprintln(os.Stderr, "Maddog entered offline Safe Mode; run `maddog repair reset-startup` before starting the desktop")
				return 78
			}
		}

		cmd := exec.Command(desktopPath(exe), os.Args[1:]...)
		cmd.Env = append(os.Environ(), "MADDOG_GUARD_PREFLIGHT=1")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		if exitErr, ok := err.(*exec.ExitError); ok && shouldRelaunch(primary, exitErr.ExitCode()) {
			continue
		}
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode()
			}
			fmt.Fprintln(os.Stderr, "maddog guard:", err)
			return 1
		}
		return 0
	}
}

func shouldRelaunch(primary bool, exitCode int) bool {
	return primary && exitCode == repair.GuardRelaunchExitCode
}

func desktopPath(guardPath string) string {
	return desktopPathForOS(guardPath, runtime.GOOS)
}

func desktopPathForOS(guardPath, goos string) string {
	name := desktopName
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(guardPath), name)
	// Debian installs the guard itself as /usr/bin/maddog-desktop and keeps
	// the Wails payload private under /usr/lib. Never recursively launch the
	// guard when the public entry point and payload share a basename.
	if filepath.Clean(path) != filepath.Clean(guardPath) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if goos == "linux" {
		return filepath.Join("/usr/lib/maddog", name)
	}
	return path
}
