//go:build windows

package repair

import "golang.org/x/sys/windows"

func replacePath(oldPath, newPath string) error {
	return windows.MoveFileEx(windows.StringToUTF16Ptr(oldPath), windows.StringToUTF16Ptr(newPath), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
