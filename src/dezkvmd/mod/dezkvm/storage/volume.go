package storage

/*
	volume.go

	Provides disk-usage statistics for a mounted filesystem, including a
	category breakdown of the files it contains (binary/executable, media,
	documents, and other).
*/

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// VolumeInfo holds disk-usage statistics for a filesystem mount point.
type VolumeInfo struct {
	TotalBytes uint64            `json:"total_bytes"`
	UsedBytes  uint64            `json:"used_bytes"`
	FreeBytes  uint64            `json:"free_bytes"`
	Categories CategoryBreakdown `json:"categories"`
}

// CategoryBreakdown classifies files into broad groups by extension.
type CategoryBreakdown struct {
	BinaryBytes uint64 `json:"binary_bytes"`
	MediaBytes  uint64 `json:"media_bytes"`
	TextBytes   uint64 `json:"text_bytes"`
	OtherBytes  uint64 `json:"other_bytes"`
}

var mediaExtensions = map[string]bool{
	".mp3":  true,
	".mp4":  true,
	".avi":  true,
	".mkv":  true,
	".mov":  true,
	".wav":  true,
	".flac": true,
	".aac":  true,
	".ogg":  true,
	".wma":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".bmp":  true,
	".tiff": true,
	".webp": true,
	".svg":  true,
	".ico":  true,
	".m4a":  true,
	".m4v":  true,
	".wmv":  true,
	".flv":  true,
	".heic": true,
	".heif": true,
}

var textExtensions = map[string]bool{
	".txt":  true,
	".md":   true,
	".csv":  true,
	".json": true,
	".xml":  true,
	".html": true,
	".htm":  true,
	".css":  true,
	".js":   true,
	".py":   true,
	".go":   true,
	".sh":   true,
	".yaml": true,
	".yml":  true,
	".toml": true,
	".ini":  true,
	".cfg":  true,
	".conf": true,
	".log":  true,
	".pdf":  true,
	".doc":  true,
	".docx": true,
	".xls":  true,
	".xlsx": true,
	".ppt":  true,
	".pptx": true,
	".odt":  true,
	".ods":  true,
	".odp":  true,
	".rtf":  true,
	".tex":  true,
}

var binaryExtensions = map[string]bool{
	".exe":  true,
	".dll":  true,
	".so":   true,
	".bin":  true,
	".elf":  true,
	".iso":  true,
	".img":  true,
	".dmg":  true,
	".deb":  true,
	".rpm":  true,
	".zip":  true,
	".tar":  true,
	".gz":   true,
	".bz2":  true,
	".xz":   true,
	".7z":   true,
	".rar":  true,
	".apk":  true,
	".msi":  true,
	".pkg":  true,
}

// GetVolumeInfo returns disk-usage statistics for the filesystem at mountPoint.
// It uses syscall.Statfs for the raw block counts and then walks the directory
// tree to build a per-category size breakdown.
func GetVolumeInfo(mountPoint string) (*VolumeInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &stat); err != nil {
		return nil, err
	}

	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	// Bavail is blocks available to unprivileged users; Bfree includes
	// reserved root-only blocks. We report what the user can actually use.
	free := stat.Bavail * bsize
	used := total - (stat.Bfree * bsize)

	cats, _ := GetFileCategoryBreakdown(mountPoint) // non-fatal if walk fails

	return &VolumeInfo{
		TotalBytes: total,
		UsedBytes:  used,
		FreeBytes:  free,
		Categories: cats,
	}, nil
}

// GetFileCategoryBreakdown walks mountPoint and classifies the total size of all
// files into binary/executable, media, text/document, and other categories.
func GetFileCategoryBreakdown(mountPoint string) (CategoryBreakdown, error) {
	var bd CategoryBreakdown
	err := filepath.Walk(mountPoint, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil // skip unreadable entries; don't abort walk
		}
		size := uint64(info.Size())
		ext := strings.ToLower(filepath.Ext(info.Name()))
		switch {
		case mediaExtensions[ext]:
			bd.MediaBytes += size
		case textExtensions[ext]:
			bd.TextBytes += size
		case binaryExtensions[ext]:
			bd.BinaryBytes += size
		default:
			bd.OtherBytes += size
		}
		return nil
	})
	return bd, err
}
