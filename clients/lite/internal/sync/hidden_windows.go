package sync

import "golang.org/x/sys/windows"

// hasHiddenAttr reports whether Windows marks the file hidden or system.
func hasHiddenAttr(full string) bool {
	name, err := windows.UTF16PtrFromString(full)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(name)
	return err == nil && attrs&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0
}
