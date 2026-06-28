//go:build windows

package codegraph

func processExists(int) bool {
	return false
}
