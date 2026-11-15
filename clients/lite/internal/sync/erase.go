package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// eraseFolderContents deletes everything inside one sync folder and keeps the
// folder itself. It is the only place the client deletes files in bulk
// (remote wipe, docs/clients.md), so it refuses anything that does not look
// like a folder the client manages:
//
//   - the folder must exist as a real directory: a symlink in its place is
//     refused, nothing behind it is touched;
//   - never a volume root, and never one of the protected paths (the user's
//     home, the client's own config directory) or a folder that contains one;
//   - links inside are removed as links, never followed (os.RemoveAll unlinks
//     them), so nothing outside the folder is reached through them.
//
// Entries that cannot be removed are skipped and reported, the rest is erased.
func eraseFolderContents(root string, protected ...string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("refusing to erase %q: not an absolute path", root)
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing to erase %s: not a plain folder", root)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if filepath.Dir(real) == real {
		return fmt.Errorf("refusing to erase %s: it is a drive root", root)
	}
	for _, path := range protected {
		if path == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		if path == real || insideRoot(real, path) {
			return fmt.Errorf("refusing to erase %s: it contains %s", root, path)
		}
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(real, entry.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
