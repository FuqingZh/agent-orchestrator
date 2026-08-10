//go:build unix

package gitworktree

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// makeOwnerDirectoriesWritable adds owner read, write, and search permissions
// only to real directories owned by AO's effective user. Directory descriptors
// are opened with O_NOFOLLOW before ownership and mode changes, so a symlink in
// an otherwise removable worktree cannot redirect chmod outside that worktree.
func makeOwnerDirectoriesWritable(root string) (bool, error) {
	repaired := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Check the entry's own type before IsDir. A symlink may target a
		// directory, but permission repair must never open or chmod that target.
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			return nil
		}

		owned, changed, err := makeOwnerDirectoryWritable(path)
		if err != nil {
			return err
		}
		if !owned {
			return fs.SkipDir
		}
		repaired = repaired || changed
		return nil
	})
	return repaired, err
}

func makeOwnerDirectoryWritable(path string) (owned, changed bool, err error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, false, err
	}
	defer unix.Close(fd) //nolint:errcheck // the directory descriptor is read-only

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, false, err
	}
	effectiveUID := uint32(os.Geteuid()) //nolint:gosec // Unix geteuid returns the kernel's non-negative uid_t value.
	if stat.Uid != effectiveUID {
		return false, false, nil
	}
	if stat.Mode&0o700 == 0o700 {
		return true, false, nil
	}
	// Stat_t.Mode is uint16 on Darwin and uint32 on Linux, while Fchmod uses
	// uint32 on both platforms.
	if err := unix.Fchmod(fd, uint32(stat.Mode)|0o700); err != nil { //nolint:unconvert // required by Darwin's Stat_t.Mode
		return true, false, err
	}
	return true, true, nil
}
