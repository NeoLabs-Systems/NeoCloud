package shellfolder

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
int ncSetFolderIcon(const char *path, const void *png, int length);
int ncSidebarAdd(const char *path);
int ncSidebarRemove(const char *path);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// setIcon gives dir a custom Finder icon (NSWorkspace writes the "Icon\r"
// file and the folder's FinderInfo; sync ignores both).
func setIcon(dir, _ string) error {
	path := C.CString(dir)
	defer C.free(unsafe.Pointer(path))
	if C.ncSetFolderIcon(path, unsafe.Pointer(&logoPNG[0]), C.int(len(logoPNG))) == 0 {
		return errors.New("Finder refused the icon")
	}
	return nil
}

// pin adds dir to the Finder sidebar's Favorites.
func pin(dir, _ string) error {
	path := C.CString(dir)
	defer C.free(unsafe.Pointer(path))
	if C.ncSidebarAdd(path) == 0 {
		return errors.New("the Finder sidebar list is not available")
	}
	return nil
}

// unpin removes dir from the Finder sidebar's Favorites.
func unpin(dir string) error {
	path := C.CString(dir)
	defer C.free(unsafe.Pointer(path))
	if C.ncSidebarRemove(path) == 0 {
		return errors.New("the Finder sidebar list is not available")
	}
	return nil
}
