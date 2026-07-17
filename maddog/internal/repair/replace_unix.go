//go:build !windows

package repair

import "os"

func replacePath(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
