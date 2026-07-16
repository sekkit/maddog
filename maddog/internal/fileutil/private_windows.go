//go:build windows

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// EnsurePrivateDir creates dir, then replaces inherited permissions with a
// protected DACL limited to the current user, SYSTEM, and Administrators.
func EnsurePrivateDir(path string) error {
	if err := rejectPrivateReparsePoint(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	clean := filepath.Clean(path)
	if clean == "." || filepath.Dir(clean) == clean {
		return nil
	}
	if err := rejectPrivateReparsePoint(path); err != nil {
		return err
	}
	return protectWindowsPath(path, true)
}

// ProtectPrivateFile replaces inherited permissions with the private DACL.
func ProtectPrivateFile(path string) error {
	if err := rejectPrivateReparsePoint(path); err != nil {
		return err
	}
	return protectWindowsPath(path, false)
}

func rejectPrivateReparsePoint(path string) error {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
		return nil
	}
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("private path must not be a reparse point: %s", path)
	}
	return nil
}

func protectWindowsPath(path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = uint32(windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
	}
	entries := []windows.EXPLICIT_ACCESS{
		privateAccessEntry(user.User.Sid, windows.TRUSTEE_IS_USER, inheritance),
		privateAccessEntry(system, windows.TRUSTEE_IS_WELL_KNOWN_GROUP, inheritance),
		privateAccessEntry(admins, windows.TRUSTEE_IS_GROUP, inheritance),
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}

func privateAccessEntry(sid *windows.SID, trusteeType windows.TRUSTEE_TYPE, inheritance uint32) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  trusteeType,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}
