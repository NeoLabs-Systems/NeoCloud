package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// FolderName is the name of the local folder the app creates for the
// whole cloud, and of the folder that holds the ones it creates for
// single cloud folders.
const FolderName = "NeoCloud"

// SuggestLocal proposes where the cloud-first flow creates the local
// folder for remote: <parent>/NeoCloud for the whole cloud, else
// <parent>/<the cloud folder's name>. An empty parent means the default:
// home for the whole cloud, home/NeoCloud for a single folder (when that
// folder is synced itself, home with "NeoCloud <name>").
//
// The answer never exists yet and never overlaps a pair: a taken name
// gets " 2", " 3", … appended.
func SuggestLocal(home, parent, remote string, pairs []Pair) (string, error) {
	remote = CleanRemote(remote)
	name := FolderName
	if remote != "" {
		name = safeFolderName(path.Base(remote))
	}
	if parent == "" {
		parent = home
		if remote != "" {
			parent = filepath.Join(home, FolderName)
			if insidePair(parent, pairs) {
				parent, name = home, FolderName+" "+name
			}
		}
	}
	parent, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	if insidePair(parent, pairs) {
		return "", fmt.Errorf("%s is already synced; choose another location", parent)
	}
	for n := 1; n < 1000; n++ {
		candidate := filepath.Join(parent, name)
		if n > 1 {
			candidate += " " + strconv.Itoa(n)
		}
		if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if !overlapsAny(candidate, pairs) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free folder name in %s", parent)
}

// safeFolderName keeps a cloud folder's name usable as a local folder on
// every desktop: characters Windows refuses become "_".
func safeFolderName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return FolderName
	}
	return name
}

// insidePair reports whether dir is a pair's folder or inside one.
func insidePair(dir string, pairs []Pair) bool {
	dir = filepath.Clean(dir)
	for _, pair := range pairs {
		root := filepath.Clean(pair.Local)
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func overlapsAny(dir string, pairs []Pair) bool {
	for _, pair := range pairs {
		if overlaps(pair.Local, dir) {
			return true
		}
	}
	return false
}

// CreateLocal makes a pair's local folder and any missing parents. It
// returns the folders it made, outermost first.
func CreateLocal(dir string) ([]string, error) {
	var missing []string
	for at := filepath.Clean(dir); ; at = filepath.Dir(at) {
		if _, err := os.Stat(at); err == nil {
			break
		}
		missing = append([]string{at}, missing...)
		if filepath.Dir(at) == at {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return missing, nil
}

// SidebarFolder is the folder the Finder sidebar / Explorer navigation
// pane shows: the NeoCloud folder of the first pair the app created
// (its own folder when that is named NeoCloud…, else the NeoCloud folder
// that holds it). "" when the app created none or the user turned the
// entry off.
func (c Config) SidebarFolder() string {
	if !c.Sidebar() {
		return ""
	}
	for _, pair := range c.Pairs {
		if !pair.Created {
			continue
		}
		if strings.HasPrefix(filepath.Base(pair.Local), FolderName) {
			return pair.Local
		}
		if parent := filepath.Dir(pair.Local); filepath.Base(parent) == FolderName {
			return parent
		}
		return pair.Local
	}
	return ""
}
