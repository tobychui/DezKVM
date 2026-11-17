package storage

/*
	mount.go

	Mount helper that makes non-ASCII file names (Chinese, Japanese, emoji...)
	show up correctly. A plain `mount` of a FAT drive uses the kernel's default
	charset (often iso8859-1), which turns every character it cannot represent
	into '?'.
*/

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// MountDevice mounts device at mountPoint with UTF-8 file name options for
// its filesystem. If the filesystem driver rejects those options, it falls
// back to a plain mount so the drive is still usable.
func MountDevice(device string, mountPoint string) error {
	fsType := getFSType(device)
	opts := utf8MountArgs(fsType)
	if len(opts) > 0 {
		args := append(append([]string{}, opts...), device, mountPoint)
		out, err := exec.Command("mount", args...).CombinedOutput()
		if err == nil {
			return nil
		}
		log.Printf("Warning: mounting %s (%s) with UTF-8 file names failed, retrying with default options: %v - %s\n",
			device, fsType, err, strings.TrimSpace(string(out)))
	}

	out, err := exec.Command("mount", device, mountPoint).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount %s → %s: %w — %s", device, mountPoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// utf8MountArgs returns the mount arguments that make the given filesystem
// return file names as UTF-8. Filesystems that store UTF-8 natively (ext4...)
// need nothing.
func utf8MountArgs(fsType string) []string {
	switch fsType {
	case "vfat", "msdos":
		// "utf8" is the FAT-specific option, iocharset=utf8 is discouraged for FAT.
		// shortname=mixed keeps the case of 8.3 names as Windows shows them.
		return []string{"-t", "vfat", "-o", "utf8,shortname=mixed"}
	case "exfat", "iso9660":
		return []string{"-o", "iocharset=utf8"}
	case "ntfs":
		if kernelSupportsFS("ntfs3") {
			return []string{"-t", "ntfs3", "-o", "iocharset=utf8"}
		}
		// ntfs-3g converts names using the process locale, which is "C" under sudo
		return []string{"-o", "locale=C.UTF-8"}
	}
	return nil
}

// getFSType returns the filesystem type of a block device as reported by lsblk
func getFSType(device string) string {
	out, err := exec.Command("lsblk", "-no", "FSTYPE", device).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// kernelSupportsFS reports whether the running kernel has a driver for fsType
func kernelSupportsFS(fsType string) bool {
	data, err := os.ReadFile("/proc/filesystems")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[len(fields)-1] == fsType {
			return true
		}
	}
	return false
}
