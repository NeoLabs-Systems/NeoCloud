package shellfolder

import (
	"errors"
	"os"
	"path/filepath"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// sidebarCLSID names NeoCloud's entry in the Explorer navigation pane.
// The uninstaller (packaging/windows/installer.nsi) removes the same keys.
const sidebarCLSID = "{9F4DC68E-FECA-4260-9BF2-A1C29417C317}"

const (
	clsidKey      = `Software\Classes\CLSID\` + sidebarCLSID
	namespaceKey  = `Software\Microsoft\Windows\CurrentVersion\Explorer\Desktop\NameSpace\` + sidebarCLSID
	hideIconsKey  = `Software\Microsoft\Windows\CurrentVersion\Explorer\HideDesktopIcons\NewStartPanel`
	delegateCLSID = "{0E5AAE11-A475-4c5b-AB00-C66DE400274E}" // the shell's folder delegate
)

var shChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

const (
	shcneUpdateItem   = 0x00002000
	shcneAssocChanged = 0x08000000
	shcnfPathW        = 0x0005
)

// setIcon writes desktop.ini pointing at the logo's .ico in assets and
// sets the attributes Explorer needs to read it (sync ignores desktop.ini).
func setIcon(dir, assets string) error {
	icon, err := writeAsset(assets, "neocloud.ico", logoICO)
	if err != nil {
		return err
	}
	ini := filepath.Join(dir, "desktop.ini")
	text := "[.ShellClassInfo]\r\nIconResource=" + icon + ",0\r\nIconFile=" + icon + "\r\nIconIndex=0\r\n"
	if err := os.WriteFile(ini, utf16File(text), 0o644); err != nil {
		return err
	}
	if err := setAttributes(ini, windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM); err != nil {
		return err
	}
	if err := setAttributes(dir, windows.FILE_ATTRIBUTE_READONLY); err != nil {
		return err
	}
	notify(shcneUpdateItem, dir)
	return nil
}

// utf16File is text as UTF-16LE with a byte order mark, so desktop.ini
// holds any path.
func utf16File(text string) []byte {
	units := append([]uint16{0xFEFF}, utf16.Encode([]rune(text))...)
	out := make([]byte, 0, 2*len(units))
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func setAttributes(path string, add uint32) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(name, attrs|add)
}

func notify(event uint32, path string) {
	var item uintptr
	if path != "" {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return
		}
		item = uintptr(unsafe.Pointer(name))
	}
	shChangeNotify.Call(uintptr(event), shcnfPathW, item, 0)
}

// pin registers dir as a navigation pane entry the way cloud storage
// providers do (a delegate folder under HKCU, no admin needed).
func pin(dir, assets string) error {
	icon, err := writeAsset(assets, "neocloud.ico", logoICO)
	if err != nil {
		return err
	}
	name := filepath.Base(dir)
	steps := []struct {
		key    string
		values func(registry.Key) error
	}{
		{clsidKey, func(k registry.Key) error {
			return errors.Join(
				k.SetStringValue("", name),
				k.SetDWordValue("System.IsPinnedToNameSpaceTree", 1),
				k.SetDWordValue("SortOrderIndex", 0x42))
		}},
		{clsidKey + `\DefaultIcon`, func(k registry.Key) error { return k.SetStringValue("", icon+",0") }},
		{clsidKey + `\InProcServer32`, func(k registry.Key) error {
			return k.SetExpandStringValue("", `%SystemRoot%\system32\shell32.dll`)
		}},
		{clsidKey + `\Instance`, func(k registry.Key) error { return k.SetStringValue("CLSID", delegateCLSID) }},
		{clsidKey + `\Instance\InitPropertyBag`, func(k registry.Key) error {
			return errors.Join(
				k.SetDWordValue("Attributes", 0x11),
				k.SetStringValue("TargetFolderPath", dir))
		}},
		{clsidKey + `\ShellFolder`, func(k registry.Key) error {
			return errors.Join(
				k.SetDWordValue("FolderValueFlags", 0x28),
				k.SetDWordValue("Attributes", 0xF080004D))
		}},
		{namespaceKey, func(k registry.Key) error { return k.SetStringValue("", name) }},
		{hideIconsKey, func(k registry.Key) error { return k.SetDWordValue(sidebarCLSID, 1) }},
	}
	for _, step := range steps {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, step.key, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = step.values(key)
		key.Close()
		if err != nil {
			return err
		}
	}
	notify(shcneAssocChanged, "")
	return nil
}

// unpin removes the navigation pane entry. dir does not matter: there is
// only the one.
func unpin(string) error {
	err := errors.Join(
		deleteTree(registry.CURRENT_USER, clsidKey),
		deleteTree(registry.CURRENT_USER, namespaceKey))
	if key, err := registry.OpenKey(registry.CURRENT_USER, hideIconsKey, registry.SET_VALUE); err == nil {
		key.DeleteValue(sidebarCLSID)
		key.Close()
	}
	notify(shcneAssocChanged, "")
	return err
}

// deleteTree deletes a key with everything below it; a missing key is fine.
func deleteTree(root registry.Key, path string) error {
	key, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	children, err := key.ReadSubKeyNames(-1)
	key.Close()
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := deleteTree(root, path+`\`+child); err != nil {
			return err
		}
	}
	return registry.DeleteKey(root, path)
}
