//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes = kernel32.NewProc("GetSystemTimes")
)

// MEMORYSTATUSEX structure for GlobalMemoryStatusEx
type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// getCPUPercent returns aggregate CPU usage as a percentage on Windows
// by calling GetSystemTimes API twice with a 200ms gap.
func getCPUPercent() float64 {
	getCPUTimes := func() (idle, kernel, user uint64) {
		var idleTime, kernelTime, userTime syscall.Filetime
		ret, _, _ := procGetSystemTimes.Call(
			uintptr(unsafe.Pointer(&idleTime)),
			uintptr(unsafe.Pointer(&kernelTime)),
			uintptr(unsafe.Pointer(&userTime)),
		)
		if ret == 0 {
			return 0, 0, 0
		}

		// Convert FILETIME to uint64 (100-nanosecond intervals)
		idle = uint64(idleTime.HighDateTime)<<32 | uint64(idleTime.LowDateTime)
		kernel = uint64(kernelTime.HighDateTime)<<32 | uint64(kernelTime.LowDateTime)
		user = uint64(userTime.HighDateTime)<<32 | uint64(userTime.LowDateTime)
		return
	}

	idle1, kernel1, user1 := getCPUTimes()
	time.Sleep(200 * time.Millisecond)
	idle2, kernel2, user2 := getCPUTimes()

	idleDelta := idle2 - idle1
	kernelDelta := kernel2 - kernel1
	userDelta := user2 - user1

	// Kernel time includes idle time, so total = kernel + user
	totalDelta := kernelDelta + userDelta
	if totalDelta == 0 {
		return 0.0
	}

	// CPU usage = (total - idle) / total
	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
}

// getRAMPercent returns RAM usage as a percentage on Windows
// by calling GlobalMemoryStatusEx.
func getRAMPercent() float64 {
	var memStatus memoryStatusEx
	memStatus.dwLength = uint32(unsafe.Sizeof(memStatus))

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")

	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret == 0 {
		return 0.0
	}

	return float64(memStatus.dwMemoryLoad)
}
