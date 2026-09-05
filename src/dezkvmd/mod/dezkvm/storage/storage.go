package storage

/*
	storage.go

	Provide storage utilities function for DezKVM, such as querying connected USB devices
	and their partition information via lsblk.

*/

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// lsblkDevice mirrors the JSON structure returned by lsblk -bJ.
type lsblkDevice struct {
	Name       string        `json:"name"`
	Model      *string       `json:"model"`
	Size       json.Number   `json:"size"`
	PTUUID     *string       `json:"ptuuid"`
	UUID       *string       `json:"uuid"` // Filesystem UUID (set on unpartitioned disks, e.g. exFAT)
	PARTUUID   *string       `json:"partuuid"`
	Label      *string       `json:"label"`
	Mountpoint *string       `json:"mountpoint"`
	FSType     *string       `json:"fstype"`
	LogSec     *json.Number  `json:"log-sec"`
	Type       string        `json:"type"`
	Children   []lsblkDevice `json:"children"`
}

type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseSize(n json.Number) uint64 {
	if n == "" {
		return 0
	}
	v, _ := strconv.ParseUint(string(n), 10, 64)
	return v
}

// resolveToDisk returns the disk-level device path for the given path.
// If devicePath is a partition (e.g. /dev/sda1) it returns the parent disk (/dev/sda).
func resolveToDisk(devicePath string) (string, error) {
	out, err := exec.Command("lsblk", "-no", "TYPE,PKNAME", devicePath).Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", errors.New("lsblk returned no output for " + devicePath)
	}
	fields := strings.Fields(strings.SplitN(line, "\n", 2)[0])
	if len(fields) >= 2 && fields[0] == "part" && fields[1] != "" {
		return "/dev/" + fields[1], nil
	}
	return devicePath, nil
}

type DiskInfo struct {
	Name       string          // Disk name (e.g. "sda")
	Model      string          // Disk model (e.g. "Samsung SSD 860")
	Size       uint64          // Size in bytes
	PTUUID     string          // Partition Table UUID (empty for unpartitioned disks)
	UUID       string          // Filesystem UUID (set on unpartitioned disks such as exFAT; empty otherwise)
	Partitions []PartitionInfo // List of partitions on this disk
}

type PartitionInfo struct {
	PARTUUID   string // Partition UUID
	Label      string // Partition label (e.g. "DezKVM_USB")
	Name       string // Partition name (e.g. "sda1")
	Mountpoint string // Mount point (e.g. "/media/usb")
	FSType     string // Filesystem type (e.g. "vfat", "ntfs", "ext4")
	BlockSize  uint64 // Block size in bytes
	Size       uint64 // Size in bytes
}

func GetDiskInfoFromDevicePath(devicePath string) (*DiskInfo, error) {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		return nil, err
	}

	out, err := exec.Command("lsblk", "-bJ", "-o",
		"NAME,MODEL,SIZE,PTUUID,UUID,TYPE,PARTUUID,LABEL,MOUNTPOINT,FSTYPE,LOG-SEC",
		diskPath).Output()
	if err != nil {
		return nil, err
	}

	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return nil, err
	}
	if len(lsblkOut.BlockDevices) == 0 {
		return nil, errors.New("no block device info returned for " + diskPath)
	}

	dev := lsblkOut.BlockDevices[0]
	info := &DiskInfo{
		Name:   dev.Name,
		Model:  derefStr(dev.Model),
		Size:   parseSize(dev.Size),
		PTUUID: derefStr(dev.PTUUID),
		UUID:   derefStr(dev.UUID),
	}

	for _, child := range dev.Children {
		if child.Type != "part" {
			continue
		}
		var blockSize uint64
		if child.LogSec != nil {
			blockSize = parseSize(*child.LogSec)
		}
		info.Partitions = append(info.Partitions, PartitionInfo{
			PARTUUID:   derefStr(child.PARTUUID),
			Label:      derefStr(child.Label),
			Name:       child.Name,
			Mountpoint: derefStr(child.Mountpoint),
			FSType:     derefStr(child.FSType),
			BlockSize:  blockSize,
			Size:       parseSize(child.Size),
		})
	}
	return info, nil
}

func GetDevicePartitionsInfoFromPath(devicePath string) ([]PartitionInfo, error) {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		return nil, err
	}

	out, err := exec.Command("lsblk", "-bJ", "-o",
		"NAME,TYPE,PARTUUID,LABEL,MOUNTPOINT,FSTYPE,SIZE,LOG-SEC",
		diskPath).Output()
	if err != nil {
		return nil, err
	}

	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return nil, err
	}
	if len(lsblkOut.BlockDevices) == 0 {
		return nil, errors.New("no block device info returned for " + diskPath)
	}

	var partitions []PartitionInfo
	for _, child := range lsblkOut.BlockDevices[0].Children {
		if child.Type != "part" {
			continue
		}
		var blockSize uint64
		if child.LogSec != nil {
			blockSize = parseSize(*child.LogSec)
		}
		partitions = append(partitions, PartitionInfo{
			PARTUUID:   derefStr(child.PARTUUID),
			Label:      derefStr(child.Label),
			Name:       child.Name,
			Mountpoint: derefStr(child.Mountpoint),
			FSType:     derefStr(child.FSType),
			BlockSize:  blockSize,
			Size:       parseSize(child.Size),
		})
	}
	return partitions, nil
}

// GetStorageDeviceLists returns a list of /dev paths for connected storage devices
// (e.g. ["/dev/sda", "/dev/sdb"]).
func GetStorageDeviceLists() ([]string, error) {
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil, err
	}
	devices := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "sd") {
			devices = append(devices, "/dev/"+name)
		}
	}
	return devices, nil
}

// ListDiskUUIDs returns a snapshot map of disk name → PTUUID/filesystem-UUID for
// every disk-type block device currently visible to the kernel.  It is used to
// detect new devices that appear after a USB-side switch.
func ListDiskUUIDs() (map[string]string, error) {
	out, err := exec.Command("lsblk", "-bJ", "-o", "NAME,PTUUID,UUID,TYPE").Output()
	if err != nil {
		return nil, err
	}
	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, dev := range lsblkOut.BlockDevices {
		if dev.Type != "disk" {
			continue
		}
		id := derefStr(dev.PTUUID)
		if id == "" {
			id = derefStr(dev.UUID)
		}
		m[dev.Name] = id
	}
	return m, nil
}

// FindNewDisk scans current block devices and returns the path and UUID of the
// first disk that is not present in the prev snapshot.  Returns an error when no
// new disk is found.
func FindNewDisk(prev map[string]string) (devPath, diskUUID string, err error) {
	current, err := ListDiskUUIDs()
	if err != nil {
		return "", "", err
	}
	for name, id := range current {
		if _, existed := prev[name]; !existed {
			return "/dev/" + name, id, nil
		}
	}
	return "", "", errors.New("no new disk device found")
}

// GetDevicePathFromPTUUID returns the /dev path of a disk whose PTUUID or
// filesystem UUID (for unpartitioned disks like exFAT) matches the given value.
func GetDevicePathFromPTUUID(ptuuid string) (string, error) {
	out, err := exec.Command("lsblk", "-bJ", "-o", "NAME,PTUUID,UUID,TYPE").Output()
	if err != nil {
		return "", err
	}

	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return "", err
	}

	for _, dev := range lsblkOut.BlockDevices {
		if dev.Type != "disk" {
			continue
		}
		if derefStr(dev.PTUUID) == ptuuid || derefStr(dev.UUID) == ptuuid {
			return "/dev/" + dev.Name, nil
		}
	}
	return "", errors.New("no disk found with PTUUID/UUID " + ptuuid)
}

// GetPTUUIDFromDevicePath returns the PTUUID (or filesystem UUID for
// unpartitioned disks) of the disk at devicePath.  This is used after a format
// operation to refresh the cached massStorageUUID, which changes whenever a new
// partition table is written.
func GetPTUUIDFromDevicePath(devicePath string) (string, error) {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		diskPath = devicePath
	}

	out, err := exec.Command("lsblk", "-bJ", "-o", "NAME,PTUUID,UUID,TYPE", diskPath).Output()
	if err != nil {
		return "", err
	}

	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return "", err
	}

	for _, dev := range lsblkOut.BlockDevices {
		if dev.Type != "disk" {
			continue
		}
		if pt := derefStr(dev.PTUUID); pt != "" {
			return pt, nil
		}
		// Unpartitioned disk: fall back to filesystem UUID
		if u := derefStr(dev.UUID); u != "" {
			return u, nil
		}
	}
	return "", errors.New("no PTUUID or UUID found for device " + diskPath)
}

// GetMountPointFromDevicePath returns the first active mount point for devicePath.
// It checks child partitions first, then the device itself (for unpartitioned
// filesystems such as exFAT drives formatted without a partition table).
func GetMountPointFromDevicePath(devicePath string) (string, error) {
	diskPath, err := resolveToDisk(devicePath)
	if err != nil {
		diskPath = devicePath
	}

	out, err := exec.Command("lsblk", "-bJ", "-o", "NAME,TYPE,MOUNTPOINT", diskPath).Output()
	if err != nil {
		return "", err
	}

	var lsblkOut lsblkOutput
	if err := json.Unmarshal(out, &lsblkOut); err != nil {
		return "", err
	}
	if len(lsblkOut.BlockDevices) == 0 {
		return "", errors.New("no block device info returned for " + diskPath)
	}

	dev := lsblkOut.BlockDevices[0]

	// Check child partitions first.
	for _, child := range dev.Children {
		if mp := derefStr(child.Mountpoint); mp != "" {
			return mp, nil
		}
	}

	// Unpartitioned disk: check the device itself.
	if mp := derefStr(dev.Mountpoint); mp != "" {
		return mp, nil
	}

	return "", errors.New("device " + devicePath + " has no active mount point")
}

// GetPTUUID returns a unique identifier for the disk containing the given device path.
// For disks with a partition table it returns the PTUUID; for unpartitioned disks
// (e.g. exFAT drives formatted without a partition table) it falls back to the
// filesystem UUID reported by lsblk.
// If a partition path (e.g. /dev/sda1) is passed, it resolves to the parent disk first.
func GetPTUUID(devicePath string) (string, error) {
	// Step 1: determine whether the device is a partition, independently of UUID queries.
	typeOut, err := exec.Command("lsblk", "-no", "TYPE", devicePath).Output()
	if err != nil {
		return "", err
	}
	devType := strings.TrimSpace(strings.SplitN(string(typeOut), "\n", 2)[0])

	targetPath := devicePath
	if devType == "part" {
		// Resolve to the parent disk via PKNAME
		pknameOut, err := exec.Command("lsblk", "-no", "PKNAME", devicePath).Output()
		if err != nil {
			return "", err
		}
		pkname := strings.TrimSpace(strings.SplitN(string(pknameOut), "\n", 2)[0])
		if pkname == "" {
			return "", errors.New("could not determine parent disk for partition " + devicePath)
		}
		targetPath = "/dev/" + pkname
	}

	// Step 2: query PTUUID and filesystem UUID on the resolved disk using key=value output
	// to avoid ambiguity when one of the fields is empty.
	pairsOut, err := exec.Command("lsblk", "-Pno", "PTUUID,UUID", targetPath).Output()
	if err != nil {
		return "", err
	}
	pairsLine := strings.TrimSpace(strings.SplitN(string(pairsOut), "\n", 2)[0])
	if pairsLine == "" {
		return "", errors.New("lsblk returned no output for " + targetPath)
	}

	extractField := func(key, s string) string {
		prefix := key + `="`
		idx := strings.Index(s, prefix)
		if idx == -1 {
			return ""
		}
		rest := s[idx+len(prefix):]
		end := strings.Index(rest, `"`)
		if end == -1 {
			return rest
		}
		return rest[:end]
	}

	if ptuuid := extractField("PTUUID", pairsLine); ptuuid != "" {
		return ptuuid, nil
	}

	//Trim the first part of the string to avoid confusion with PTUUID when UUID is empty
	//e.g. PTUUID="" UUID="1234-5678" -> after trimming PTUUID="" we get UUID="1234-5678"
	if idx := strings.Index(pairsLine, `PTUUID="`); idx != -1 {
		pairsLine = pairsLine[idx+len(`PTUUID="" `):]
	}

	// Fallback: unpartitioned disk (e.g. exFAT) — use filesystem UUID
	if fsuuid := extractField("UUID", pairsLine); fsuuid != "" {
		return fsuuid, nil
	}

	return "", errors.New("no PTUUID or UUID found for " + targetPath)
}
