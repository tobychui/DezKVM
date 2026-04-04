package usbcapture

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

/*
	1920 x 1080 60fps = 55Mbps //Edge not support
	1920 x 1080 30fps = 50Mbps
	1920 x 1080 25fps = 40Mbps
	1920 x 1080 20fps = 30Mbps
	1920 x 1080 10fps = 15Mbps

	1360 x 768 60fps = 28Mbps
	1360 x 768 30fps = 25Mbps
	1360 x 768 25fps = 20Mbps
	1360 x 768 20fps = 18Mbps
	1360 x 768 10fps = 10Mbps
*/

// Struct to store the size and fps info
type FormatInfo struct {
	Format string
	Sizes  []SizeInfo
}

type SizeInfo struct {
	Width  int
	Height int
	FPS    []int
}

// CheckVideoCaptureDevice checks if the given video device is a video capture device
func checkVideoCaptureDevice(device string) (bool, error) {
	// Run v4l2-ctl to get device capabilities
	cmd := exec.Command("v4l2-ctl", "--device", device, "--all")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to execute v4l2-ctl: %w", err)
	}

	// Convert output to string and check for the "Video Capture" capability
	outputStr := string(output)
	if strings.Contains(outputStr, "Video Capture") {
		return true, nil
	}
	return false, nil
}

func PrintV4L2FormatInfo(devicePath string) {
	// Check if the device is a video capture device
	isCapture, err := checkVideoCaptureDevice(devicePath)
	if err != nil {
		fmt.Printf("Error checking device: %v\n", err)
		return
	}
	if !isCapture {
		fmt.Printf("Device %s is not a video capture device\n", devicePath)
		return
	}

	// Get format info
	formats, err := GetV4L2FormatInfo(devicePath)
	if err != nil {
		fmt.Printf("Error getting format info: %v\n", err)
		return
	}

	// Print format info
	for _, format := range formats {
		fmt.Printf("Format: %s\n", format.Format)
		for _, size := range format.Sizes {
			fmt.Printf("  Size: %dx%d\n", size.Width, size.Height)
			fmt.Printf("    FPS: %v\n", size.FPS)
		}
	}
}

// Function to run the v4l2-ctl command and parse the output
func GetV4L2FormatInfo(devicePath string) ([]FormatInfo, error) {
	// Run the v4l2-ctl command to list formats
	cmd := exec.Command("v4l2-ctl", "--list-formats-ext", "-d", devicePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	// Parse the output
	var formats []FormatInfo
	var currentFormat *FormatInfo
	scanner := bufio.NewScanner(&out)

	formatRegex := regexp.MustCompile(`\[(\d+)\]: '(\S+)'`)
	sizeRegex := regexp.MustCompile(`Size: Discrete (\d+)x(\d+)`)
	intervalRegex := regexp.MustCompile(`Interval: Discrete (\d+\.\d+)s \((\d+\.\d+) fps\)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Match format line
		if matches := formatRegex.FindStringSubmatch(line); matches != nil {
			if currentFormat != nil {
				formats = append(formats, *currentFormat)
			}
			// Start a new format entry
			currentFormat = &FormatInfo{
				Format: matches[2],
			}
		}

		// Match size line
		if matches := sizeRegex.FindStringSubmatch(line); matches != nil {
			width, _ := strconv.Atoi(matches[1])
			height, _ := strconv.Atoi(matches[2])

			// Initialize the size entry
			sizeInfo := SizeInfo{
				Width:  width,
				Height: height,
			}

			// Match FPS intervals for the current size
			for scanner.Scan() {
				line = scanner.Text()
				if fpsMatches := intervalRegex.FindStringSubmatch(line); fpsMatches != nil {
					fps, _ := strconv.ParseInt(fpsMatches[2], 10, 0)
					sizeInfo.FPS = append(sizeInfo.FPS, int(fps))
				} else {
					// Stop parsing FPS intervals when no more matches are found
					break
				}
			}
			// Add the size information to the current format
			currentFormat.Sizes = append(currentFormat.Sizes, sizeInfo)
		}
	}

	// Append the last format if present
	if currentFormat != nil {
		formats = append(formats, *currentFormat)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return formats, nil
}
