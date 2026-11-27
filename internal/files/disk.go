package files

import "golang.org/x/sys/unix"

// CheckDiskSpace refuses to write when `needed` more bytes would take the
// storage disk below its floor, so a full disk never leaves the database or a
// half-written blob behind.
func (h *Handler) CheckDiskSpace(needed int64) error {
	var stats unix.Statfs_t
	if h.minFreeDiskBytes <= 0 || unix.Statfs(h.blobDir, &stats) != nil {
		return nil
	}
	free := uint64(stats.Bavail) * uint64(stats.Bsize)
	wanted := uint64(max(needed, 0))
	if free < wanted || free-wanted < uint64(h.minFreeDiskBytes) {
		return apiError{507, "NeoCloud is out of space right now. Try again later or contact the person who runs it.", "DISK_FULL"}
	}
	return nil
}
