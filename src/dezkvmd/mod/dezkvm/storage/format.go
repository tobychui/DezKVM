package storage

/*
	format.go

	Provides disk-formatting utilities for USB mass-storage devices.
	Supported filesystems: fat32, exfat, ext4, ntfs.

	A disk can be formatted either with a single partition spanning the whole
	device (FormatDisk) or with a user-defined partition layout of up to
	MaxPartitions partitions (FormatDiskParts).

	All operations use exec.Command with explicit argument lists (never a shell
	string) to prevent command-injection attacks.  The filesystem type and
	partition label are validated against strict allowlists before any command
	is executed.
*/

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SupportedFilesystems is the set of filesystem types that FormatDisk accepts.
var SupportedFilesystems = []string{"fat32", "exfat", "ext4", "ntfs"}

// MaxPartitions is the maximum number of partitions FormatDiskParts accepts.
// Four keeps the layout valid for both msdos (primary-partition limit) and gpt.
const MaxPartitions = 4

// labelMaxLen defines the maximum label length per filesystem type.
var labelMaxLen = map[string]int{
	"fat32": 11,
	"exfat": 15,
	"ext4":  16,
	"ntfs":  32,
}

// labelRe matches characters that are safe in filesystem labels.
// We allow ASCII letters, digits, spaces, hyphens, and underscores only.
var labelRe = regexp.MustCompile(`^[A-Za-z0-9 _\-]*$`)

// PartitionSpec describes one user-requested partition for FormatDiskParts.
type PartitionSpec struct {
	SizeMB     uint64 `json:"size_mb"`    // partition size in MiB; 0 = use all remaining space (only allowed on the last partition)
	Filesystem string `json:"filesystem"` // one of SupportedFilesystems
	Label      string `json:"label"`      // optional volume label
}

// FormatPreview describes the partition layout that FormatDiskParts would
// produce, without actually writing anything to the disk.
type FormatPreview struct {
	Disk       string             `json:"disk"`        // e.g. "/dev/sda"
	DiskSizeB  uint64             `json:"disk_size_b"` // total disk size in bytes
	PartTable  string             `json:"part_table"`  // "gpt" or "msdos"
	FSType     string             `json:"fstype"`      // filesystem of the first partition (legacy field)
	Label      string             `json:"label"`       // label of the first partition (legacy field)
	Partitions []PreviewPartition `json:"partitions"`
}

// PreviewPartition is one row in the preview partition table.
type PreviewPartition struct {
	Name      string `json:"name"`   // e.g. "/dev/sda1"
	SizeB     uint64 `json:"size_b"` // usable size in bytes
	FSType    string `json:"fstype"` // filesystem type string shown to the user
	Label     string `json:"label"`
	PartTable string `json:"part_table"` // "gpt" or "msdos"
}

// partitionLayout is the fully validated on-disk placement of one partition.
type partitionLayout struct {
	fs       string // canonical filesystem type
	label    string // sanitised label
	startMiB uint64
	endMiB   uint64 // 0 = extends to 100 % of the disk
	sizeB    uint64
	path     string // partition device node, e.g. /dev/sda1
}

// validateFormatArgs validates and sanitises the fsType and label arguments.
// It returns the canonical fsType string and sanitised label, or an error.
func validateFormatArgs(fsType, label string) (string, string, error) {
	fs := strings.ToLower(strings.TrimSpace(fsType))
	found := false
	for _, s := range SupportedFilesystems {
		if fs == s {
			found = true
			break
		}
	}
	if !found {
		return "", "", fmt.Errorf("unsupported filesystem %q: must be one of %s",
			fsType, strings.Join(SupportedFilesystems, ", "))
	}

	lbl := strings.TrimSpace(label)
	if !labelRe.MatchString(lbl) {
		return "", "", errors.New("label contains invalid characters; use only letters, digits, spaces, hyphens and underscores")
	}
	max := labelMaxLen[fs]
	if len(lbl) > max {
		return "", "", fmt.Errorf("label too long for %s: maximum %d characters", fs, max)
	}

	return fs, lbl, nil
}

// partTableForFS returns the partition table type for the given filesystem.
// fat32, exfat, and ntfs use MBR (msdos) for maximum Windows compatibility;
// ext4 uses GPT (Linux-only, benefits from GPT robustness).
func partTableForFS(fs string) string {
	switch fs {
	case "fat32", "exfat", "ntfs":
		return "msdos"
	default:
		return "gpt"
	}
}

// resolvePartTable maps the user-requested partition table string to a
// canonical parted label type.  An empty string selects automatically:
// single-partition layouts use the per-filesystem default, multi-partition
// layouts use msdos for maximum compatibility.
func resolvePartTable(requested string, firstFS string, partitionCount int) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "auto":
		if partitionCount == 1 {
			return partTableForFS(firstFS), nil
		}
		return "msdos", nil
	case "msdos", "mbr":
		return "msdos", nil
	case "gpt":
		return "gpt", nil
	}
	return "", fmt.Errorf("unsupported partition table %q: must be msdos or gpt", requested)
}

// mkpartFSHint returns the filesystem type hint passed to "parted mkpart" for
// msdos (MBR) tables. This sets the partition type byte that Windows uses to
// identify the filesystem:
//   - fat32 → "fat32"  (type 0x0C)
//   - exfat → "ntfs"   (type 0x07 – Windows reads the actual FS from the BPB)
//   - ntfs  → "ntfs"   (type 0x07)
//   - ext4  → "ext4"   (type 0x83)
func mkpartFSHint(fs string) string {
	switch fs {
	case "fat32":
		return "fat32"
	case "exfat", "ntfs":
		return "ntfs"
	case "ext4":
		return "ext4"
	default:
		return ""
	}
}

// fstypeDisplayName returns the human-readable label for a filesystem type.
func fstypeDisplayName(fs string) string {
	switch fs {
	case "fat32":
		return "FAT32"
	case "exfat":
		return "exFAT"
	case "ext4":
		return "ext4"
	case "ntfs":
		return "NTFS"
	default:
		return fs
	}
}

// partitionDevicePath returns the device node of the n-th (1-based) partition
// of diskPath, inserting the "p" separator required by devices whose name ends
// with a digit (e.g. /dev/mmcblk0 → /dev/mmcblk0p1).
func partitionDevicePath(diskPath string, n int) string {
	if len(diskPath) > 0 {
		last := diskPath[len(diskPath)-1]
		if last >= '0' && last <= '9' {
			return fmt.Sprintf("%sp%d", diskPath, n)
		}
	}
	return fmt.Sprintf("%s%d", diskPath, n)
}

// getDiskSizeBytes returns the total size of diskPath in bytes via lsblk.
func getDiskSizeBytes(diskPath string) (uint64, error) {
	out, err := exec.Command("lsblk", "-bno", "SIZE", diskPath).Output()
	if err != nil {
		return 0, fmt.Errorf("lsblk failed: %w", err)
	}
	var diskSize uint64
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			v, parseErr := strconv.ParseUint(line, 10, 64)
			if parseErr == nil && v > diskSize {
				diskSize = v
			}
			break
		}
	}
	if diskSize == 0 {
		return 0, errors.New("could not determine disk size for " + diskPath)
	}
	return diskSize, nil
}

// computeLayout validates the requested partition specs against the disk size
// and returns the resolved placement of every partition.  The first partition
// starts at 1 MiB for alignment; only the last partition may have SizeMB == 0
// (meaning "use all remaining space").
func computeLayout(diskPath string, diskSizeB uint64, parts []PartitionSpec) ([]partitionLayout, error) {
	if len(parts) == 0 {
		return nil, errors.New("at least one partition is required")
	}
	if len(parts) > MaxPartitions {
		return nil, fmt.Errorf("too many partitions: maximum %d", MaxPartitions)
	}

	const mib = uint64(1024 * 1024)
	diskMiB := diskSizeB / mib
	if diskMiB <= 1 {
		return nil, errors.New("disk is too small to partition")
	}

	layout := make([]partitionLayout, 0, len(parts))
	cursor := uint64(1) // reserve 1 MiB at the start for alignment
	for i, p := range parts {
		fs, lbl, err := validateFormatArgs(p.Filesystem, p.Label)
		if err != nil {
			return nil, fmt.Errorf("partition %d: %w", i+1, err)
		}

		entry := partitionLayout{
			fs:       fs,
			label:    lbl,
			startMiB: cursor,
			path:     partitionDevicePath(diskPath, i+1),
		}

		if p.SizeMB == 0 {
			// Fill the rest of the disk; only valid for the last partition.
			if i != len(parts)-1 {
				return nil, fmt.Errorf("partition %d: only the last partition may leave its size empty", i+1)
			}
			if cursor >= diskMiB {
				return nil, errors.New("no space left on disk for the last partition")
			}
			entry.endMiB = 0
			entry.sizeB = (diskMiB - cursor) * mib
		} else {
			end := cursor + p.SizeMB
			if end > diskMiB {
				return nil, fmt.Errorf("partition %d: requested layout exceeds the disk size (%d MiB)", i+1, diskMiB)
			}
			entry.endMiB = end
			entry.sizeB = p.SizeMB * mib
			cursor = end
		}
		layout = append(layout, entry)
	}
	return layout, nil
}

// GetFormatPreviewParts returns the partition layout that FormatDiskParts
// would create, without modifying the disk.  devicePath must be a block-device
// path such as "/dev/sda".  partTable may be "" (auto), "msdos" or "gpt".
func GetFormatPreviewParts(devicePath, partTable string, parts []PartitionSpec) (*FormatPreview, error) {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve device path: %w", err)
	}

	diskSize, err := getDiskSizeBytes(diskPath)
	if err != nil {
		return nil, err
	}

	layout, err := computeLayout(diskPath, diskSize, parts)
	if err != nil {
		return nil, err
	}

	ptType, err := resolvePartTable(partTable, layout[0].fs, len(layout))
	if err != nil {
		return nil, err
	}

	preview := &FormatPreview{
		Disk:      diskPath,
		DiskSizeB: diskSize,
		PartTable: ptType,
		FSType:    layout[0].fs,
		Label:     layout[0].label,
	}
	for _, entry := range layout {
		preview.Partitions = append(preview.Partitions, PreviewPartition{
			Name:      entry.path,
			SizeB:     entry.sizeB,
			FSType:    fstypeDisplayName(entry.fs),
			Label:     entry.label,
			PartTable: ptType,
		})
	}
	return preview, nil
}

// GetFormatPreview returns the single-partition layout that FormatDisk would
// create, without modifying the disk.
func GetFormatPreview(devicePath, fsType, label string) (*FormatPreview, error) {
	return GetFormatPreviewParts(devicePath, "", []PartitionSpec{{Filesystem: fsType, Label: label}})
}

// FormatDiskParts partitions and formats devicePath with the requested layout.
//
// The sequence of operations is:
//  1. Unmount every mounted partition on the disk.
//  2. Write a fresh partition table with parted.
//  3. Create one primary partition per requested spec.
//  4. Run the appropriate mkfs tool on every new partition.
//
// The disk must be on the KVM side (i.e. not currently connected to the remote
// host) before this function is called – that invariant is enforced by the
// handler layer.
func FormatDiskParts(devicePath, partTable string, parts []PartitionSpec) error {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		return fmt.Errorf("cannot resolve device path: %w", err)
	}

	diskSize, err := getDiskSizeBytes(diskPath)
	if err != nil {
		return err
	}

	layout, err := computeLayout(diskPath, diskSize, parts)
	if err != nil {
		return err
	}

	ptType, err := resolvePartTable(partTable, layout[0].fs, len(layout))
	if err != nil {
		return err
	}

	// Step 1 – unmount all partitions on the disk.
	if err := UnmountAllPartitions(diskPath); err != nil {
		return fmt.Errorf("failed to unmount partitions: %w", err)
	}

	// Step 2 – create a new partition table.
	if err := runCmd("parted", "-s", diskPath, "mklabel", ptType); err != nil {
		return fmt.Errorf("parted mklabel failed: %w", err)
	}

	// Step 3 – create the requested partitions.
	for _, entry := range layout {
		mkpartArgs := []string{"-s", diskPath, "mkpart", "primary"}
		if ptType == "msdos" {
			if hint := mkpartFSHint(entry.fs); hint != "" {
				mkpartArgs = append(mkpartArgs, hint)
			}
		}
		end := "100%"
		if entry.endMiB != 0 {
			end = fmt.Sprintf("%dMiB", entry.endMiB)
		}
		mkpartArgs = append(mkpartArgs, fmt.Sprintf("%dMiB", entry.startMiB), end)
		if err := runCmd("parted", mkpartArgs...); err != nil {
			return fmt.Errorf("parted mkpart failed: %w", err)
		}
	}

	// Give the kernel a moment to register the new partitions, then poll until
	// every device node actually appears.  This prevents mkfs from failing
	// with "open failed: No such file or directory" on fast systems.
	RescanPartitions(diskPath)
	for _, entry := range layout {
		if err := waitForDevice(entry.path, 15*time.Second); err != nil {
			return fmt.Errorf("partition device %s did not appear after formatting: %w", entry.path, err)
		}
	}

	// Step 4 – format every new partition.
	for _, entry := range layout {
		if err := makeFilesystem(entry.path, entry.fs, entry.label); err != nil {
			return err
		}
	}

	return nil
}

// FormatDisk formats devicePath with a single partition spanning the whole
// disk using the requested filesystem.
func FormatDisk(devicePath, fsType, label string) error {
	return FormatDiskParts(devicePath, "", []PartitionSpec{{Filesystem: fsType, Label: label}})
}

// makeFilesystem runs the mkfs tool matching fs on partPath.  fs must already
// be validated against SupportedFilesystems.
func makeFilesystem(partPath, fs, lbl string) error {
	switch fs {
	case "fat32":
		args := []string{"-F", "32"}
		if lbl != "" {
			args = append(args, "-n", lbl)
		}
		args = append(args, partPath)
		if err := runCmd("mkfs.vfat", args...); err != nil {
			return fmt.Errorf("mkfs.vfat failed: %w", err)
		}

	case "exfat":
		args := []string{}
		if lbl != "" {
			args = append(args, "-n", lbl)
		}
		args = append(args, partPath)
		if err := runCmd("mkfs.exfat", args...); err != nil {
			return fmt.Errorf("mkfs.exfat failed: %w", err)
		}

	case "ext4":
		args := []string{"-F"} // -F: force (non-interactive)
		if lbl != "" {
			args = append(args, "-L", lbl)
		}
		args = append(args, partPath)
		if err := runCmd("mkfs.ext4", args...); err != nil {
			return fmt.Errorf("mkfs.ext4 failed: %w", err)
		}

	case "ntfs":
		args := []string{"-f"} // -f: fast format (no zero-fill)
		if lbl != "" {
			args = append(args, "-L", lbl)
		}
		args = append(args, partPath)
		if err := runCmd("mkfs.ntfs", args...); err != nil {
			return fmt.Errorf("mkfs.ntfs failed: %w", err)
		}

	default:
		return fmt.Errorf("unsupported filesystem %q", fs)
	}
	return nil
}

// listMountedPartitions returns the /dev paths of every mounted partition
// (or the disk itself for superfloppy mounts) belonging to diskPath.
func listMountedPartitions(diskPath string) []string {
	out, _ := exec.Command("lsblk", "-lno", "NAME,MOUNTPOINT", diskPath).Output()
	mounted := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mountpoint := strings.TrimSpace(fields[1])
		if mountpoint == "" || mountpoint == "[SWAP]" {
			continue
		}
		mounted = append(mounted, "/dev/"+strings.TrimSpace(fields[0]))
	}
	return mounted
}

// UnmountAllPartitions unmounts every mounted partition belonging to diskPath.
// It returns an error when any partition remains mounted afterwards: writing
// a new partition table or disk image over a mounted filesystem corrupts it,
// so callers must treat a failed unmount as fatal.
func UnmountAllPartitions(diskPath string) error {
	for _, devPath := range listMountedPartitions(diskPath) {
		// Errors are surfaced by the re-check below (the device may have
		// been unmounted by someone else between the scan and this call).
		_ = runCmd("umount", devPath)
	}

	// Verify nothing on the disk is still mounted.
	if still := listMountedPartitions(diskPath); len(still) > 0 {
		return fmt.Errorf("device(s) still mounted after unmount attempt: %s (close any programs using the drive and retry)",
			strings.Join(still, ", "))
	}
	return nil
}

// RescanPartitions asks the kernel and udev to re-read the partition table of
// diskPath after it has been rewritten (by parted or by writing a disk image).
// Both steps are best-effort: partprobe may not exist on every system.
func RescanPartitions(diskPath string) {
	_ = runCmd("partprobe", diskPath)
	_ = runCmd("udevadm", "settle", "--timeout=10")
}

// runCmd executes an external command and returns a combined-output error on failure.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w — %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// waitForDevice polls for path to appear as a block device, up to timeout.
// It is used after partprobe/udevadm settle to ensure the partition node is
// present before mkfs is called.
func waitForDevice(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to appear (waited %s)", path, timeout)
}
