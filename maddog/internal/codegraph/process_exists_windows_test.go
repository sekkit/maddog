//go:build windows

package codegraph

import "os"

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
