package dezkvm

import (
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"imuslab.com/dezkvm/dezkvmd/mod/dezkvm/storage"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmaux"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmhid"
	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
)

func (i *UsbKvmDeviceInstance) UUID() string {
	return i.uuid
}

// MassStorageUUID returns the partition-table UUID of this instance's mass
// storage device. It namespaces per-device state that must survive the drive
// being switched between the KVM and the remote computer -- the thumbnail
// cache, for one.
func (i *UsbKvmDeviceInstance) MassStorageUUID() string {
	return i.massStorageUUID
}

func (i *UsbKvmDeviceInstance) Start() error {
	if i.Config.USBKVMDevicePath == "" {
		return errors.New("USB KVM device path is not specified")
	}
	if i.Config.USBKVMBaudrate == 0 {
		//Use default baudrate if not specified
		i.Config.USBKVMBaudrate = 115200
	}

	/*  Start HID Controller  */
	usbKVM := kvmhid.NewHIDController(&kvmhid.Config{
		PortName:          i.Config.USBKVMDevicePath,
		BaudRate:          i.Config.USBKVMBaudrate,
		ScrollSensitivity: 0x01, // Set mouse scroll sensitivity
	})

	//Start the HID controller
	err := usbKVM.Connect()
	if err != nil {
		return err
	}

	i.usbKVMController = usbKVM

	/*  Start AuxMCU Controller  */
	//Check if AuxMCU is configured, if so, start the connection
	if i.Config.AuxMCUDevicePath != "" {
		if i.Config.AuxMCUBaudrate == 0 {
			//Use default baudrate if not specified
			i.Config.AuxMCUBaudrate = 115200
		}

		auxMCU, err := kvmaux.NewAuxOutbandController(i.Config.AuxMCUDevicePath, i.Config.AuxMCUBaudrate)
		if err != nil {
			return err
		}
		i.auxMCUController = auxMCU

		//Try to get the UUID from the AuxMCU
		uuid, err := auxMCU.GetUUID()
		if err != nil {
			return err
		}
		i.uuid = uuid

	} else {
		// Randomly generate a UUIDv4 if AuxMCU is not present
		uuid, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		i.uuid = uuid.String()
		i.uuid = "10" + i.uuid[2:] //USB KVM device category is "1", type is "0" (unknown/unspecified)
	}

	/*  Mount USB Mass Storage if enabled  */
	// Snapshot the disks that are already present before we switch the USB
	// mux so that we can detect which new device node appears afterwards.
	prevDisks, _ := storage.ListDiskUUIDs()

	// Switch the mass storage side to KVM side on initiate
	log.Printf("Switching mass storage to KVM side for instance %s\n", i.uuid)
	i.auxMCUController.SwitchUSBToKVM()
	// Poll up to 30 times (100 ms each) for the device to become visible.
	// Exit early as soon as it is found to avoid unnecessary delay.
	const maxAttempts = 30
	for attempt := 0; attempt < maxAttempts; attempt++ {
		time.Sleep(100 * time.Millisecond)

		if i.massStorageUUID != "" {
			// Known device: check whether the kernel can already see it.
			if _, err := storage.GetDevicePathFromPTUUID(i.massStorageUUID); err == nil {
				break
			}
		} else {
			// Unknown device: look for a disk that wasn't in the pre-switch snapshot.
			if _, newUUID, err := storage.FindNewDisk(prevDisks); err == nil {
				if strings.TrimSpace(newUUID) == "" {
					// Wait a few more cycles to see if it shows up with a valid UUID. Sometimes lsblk can report a new disk before the partition table is fully ready, resulting in an empty UUID.
					// For example lsblk output might look like this in the first few cycles:
					// 1st grab: map[mmcblk0:27fd6190 mtdblock0: sda: sdb:004ee9af zram0: zram1:]
					// 2nd grab: map[mmcblk0:27fd6190 mtdblock0: sda:5ce7de72 sdb:004ee9af zram0: zram1:]
					continue
				}
				i.massStorageUUID = newUUID
				log.Printf("Discovered new mass storage device with UUID %s for instance %s\n", newUUID, i.uuid)
				break
			}
		}
	}

	// Try to mount it

	if devPath, err := storage.GetDevicePathFromPTUUID(i.massStorageUUID); err == nil {
		log.Println("Trying to mount mass storage device: ", devPath)
	}

	mountPoint, err := i.MountMassStorage()
	if err != nil {
		log.Println("Failed to mount mass storage device: ", err)
		i.Config.EnableMassStorage = false
	} else {
		log.Printf("Mass storage mounted at %s for instance %s\n", mountPoint, i.uuid)
		i.Config.EnableMassStorage = true

		// Populate / refresh massStorageUUID from the live device so the cached
		// value always matches the current partition table UUID.  This covers
		// first-boot (empty config) and stale-UUID situations alike.
		if devPath, devErr := i.GetMassStoragePath(); devErr == nil {
			if freshUUID, uuidErr := storage.GetPTUUIDFromDevicePath(devPath); uuidErr == nil && freshUUID != "" && freshUUID != i.massStorageUUID {
				log.Printf("Updated massStorageUUID from %q to %q for instance %s\n", i.massStorageUUID, freshUUID, i.uuid)
				i.massStorageUUID = freshUUID
			}
		}
	}

	/*  Start USB Capture Device  */
	usbCaptureDevice, err := usbcapture.NewInstance(i.captureConfig)
	if err != nil {
		return err
	}

	err = usbCaptureDevice.StartVideoCapture(i.videoResoltuionConfig)
	if err != nil {
		usbCaptureDevice.Close()
		return err
	}
	i.usbCaptureDevice = usbCaptureDevice

	/*  Load Preferences  */
	if i.parent != nil {
		prefs, err := i.parent.LoadPreferences(i.uuid)
		if err != nil {
			log.Printf("Warning: failed to load preferences for %s: %v\n", i.uuid, err)
		}
		if prefs != nil {
			i.Preferences = prefs
		}
	}
	if i.Preferences == nil {
		i.Preferences = DefaultPreferences()
	}
	i.ApplyPreferences()

	// All components started successfully — turn off the status LED
	if i.auxMCUController != nil {
		_ = i.auxMCUController.SetStatusLED(kvmaux.StatusLEDOff)
	}
	return nil
}

func (i *UsbKvmDeviceInstance) Stop() error {
	// Close any active WebRTC session first (it holds the capture stream)
	i.closeWebRTCSession()

	// Unmount mass storage if it's currently mounted
	if i.massStorageMountPoint != "" {
		err := i.UnmountMassStorage()
		if err != nil {
			log.Printf("Warning: failed to unmount mass storage for instance %s: %v\n", i.uuid, err)
		}
	}

	// Close HID controller
	if i.usbKVMController != nil {
		i.usbKVMController.Close()
		i.usbKVMController = nil
	}

	// Close AuxMCU controller
	if i.auxMCUController != nil {
		i.auxMCUController.Close()
		i.auxMCUController = nil
	}

	// Close USB capture device
	if i.usbCaptureDevice != nil {
		i.usbCaptureDevice.Close()
		i.usbCaptureDevice = nil
	}
	return nil
}

// Remove removes the USB KVM device instance from its parent DezkVM manager.
func (i *UsbKvmDeviceInstance) Remove() error {
	return i.parent.RemoveUsbKvmDevice(i.UUID())
}

func (i *UsbKvmDeviceInstance) SetLEDStatus(status kvmaux.StatusLEDPattern) error {
	if i.auxMCUController == nil {
		return errors.New("AuxMCU controller is not initialized")
	}
	return i.auxMCUController.SetStatusLED(status)
}

// ApplyPreferences applies the current preferences to the running HID controller.
func (i *UsbKvmDeviceInstance) ApplyPreferences() {
	if i.Preferences == nil || i.usbKVMController == nil {
		return
	}
	// Scroll sensitivity
	sens := i.Preferences.ScrollSensitivity
	if sens == 0 {
		sens = 1
	}
	i.usbKVMController.Config.ScrollSensitivity = sens

	// Invert scroll direction
	i.usbKVMController.Config.InvertScrollDirection = i.Preferences.InvertScrollDirection

	// Mouse jiggler
	if i.Preferences.EnableMouseJiggler {
		i.usbKVMController.StartMouseJiggler()
	} else {
		i.usbKVMController.StopMouseJiggler()
	}
}

// GetMassStorageMountPoint returns the filesystem mount point of the mass storage
// device associated with this instance. It returns the cached mount point set by
// MountMassStorage when available; otherwise it queries lsblk for an active mount
// and caches the result for future calls.
// Returns an error if mass storage is not configured or the device is not mounted.
func (i *UsbKvmDeviceInstance) GetMassStorageMountPoint() (string, error) {
	if i.massStorageMountPoint != "" {
		return i.massStorageMountPoint, nil
	}
	devPath, err := i.GetMassStoragePath()
	if err != nil {
		return "", err
	}
	mp, err := storage.GetMountPointFromDevicePath(devPath)
	if err != nil {
		return "", err
	}
	// Cache so subsequent calls don't need another lsblk scan.
	i.massStorageMountPoint = mp
	return mp, nil
}

// IsStorageBrowsable reports whether the mass storage device is currently
// mounted with a filesystem that can be browsed by the file manager. It
// returns false when the drive is on the remote side, not mounted, or the
// mount point cannot be listed (e.g. no / unknown filesystem on the disk).
func (i *UsbKvmDeviceInstance) IsStorageBrowsable() bool {
	mountPoint, err := i.GetMassStorageMountPoint()
	if err != nil || mountPoint == "" {
		return false
	}
	f, err := os.Open(mountPoint)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.ReadDir(1)
	// An empty directory returns io.EOF and is still browsable.
	return err == nil || err == io.EOF
}

// Return the current mass storage device path (e.g. /dev/sdb) if mass storage is enabled for this instance,
// or return an empty string if mass storage is not enabled or the device is not found.
func (i *UsbKvmDeviceInstance) GetMassStoragePath() (string, error) {
	if i.massStorageUUID == "" {
		return "", errors.New("mass storage not configured for this instance")
	}

	devPath, err := storage.GetDevicePathFromPTUUID(i.massStorageUUID)
	if err != nil {
		return "", err
	}
	return devPath, nil
}

// MountMassStorage mount the USB mass storage attached to this USB
// KVM instance to a location where DezKVM can access it, return
// the mount point path or an error if mounting fails.
func (i *UsbKvmDeviceInstance) MountMassStorage() (string, error) {
	if i.auxMCUController.GetUSBMassStorageSide() != kvmaux.USB_MASS_STORAGE_KVM {
		log.Println("Mass storage is not on the KVM side, cannot mount.")
		return "", errors.New("mass storage is not currently on the KVM side")
	}

	devPath, err := i.GetMassStoragePath() //Should return something like /dev/sdb
	if err != nil {
		log.Println("Failed to get mass storage device path for mounting:", err)
		return "", err
	}

	// Create the mount folder for this instance's mass storage if it doesn't exist
	expectedMountPoint := "./mnt/" + i.massStorageUUID
	err = os.MkdirAll(expectedMountPoint, 0755)
	if err != nil {
		return "", err
	}

	// If the disk has partitions (e.g. /dev/sdb1), mount the first partition
	// instead of the raw disk. Unpartitioned disks (e.g. exFAT superfloppy)
	// are mounted directly.
	mountTarget := devPath
	partitions, err := storage.GetDevicePartitionsInfoFromPath(devPath)
	if err == nil && len(partitions) >= 1 {
		mountTarget = "/dev/" + partitions[0].Name
	}

	// Check if it has an active mount point already, if so, return it instead of trying to mount again
	activeMountPoint, err := storage.GetMountPointFromDevicePath(mountTarget)
	if err == nil {
		i.massStorageMountPoint = activeMountPoint
		return activeMountPoint, nil
	}

	// Mount it using the system mount command
	cmd := exec.Command("mount", mountTarget, expectedMountPoint)
	err = cmd.Run()
	if err != nil {
		return "", err
	}

	i.massStorageMountPoint = expectedMountPoint
	return expectedMountPoint, nil
}

// UnmountMassStorage unmounts the currently mounted mass storage device for this instance.
// Returns an error if unmounting fails.
func (i *UsbKvmDeviceInstance) UnmountMassStorage() error {
	if i.massStorageMountPoint == "" {
		return errors.New("mass storage is not mounted")
	}

	cmd := exec.Command("umount", i.massStorageMountPoint)
	err := cmd.Run()
	if err != nil {
		return err
	}

	i.massStorageMountPoint = ""
	return nil
}
