package thumbnails

/*
	localfs.go

	The renderers in this package were lifted from arozos, where every file
	access goes through a pluggable filesystem abstraction (local disk, SMB,
	WebDAV, ...). DezKVM only ever renders files off the locally mounted USB
	mass-storage device, so instead of porting that abstraction this file
	provides a minimal stand-in with the same method names. That keeps the
	renderer sources close to their upstream form, which matters when pulling
	in fixes from arozos later.
*/

import (
	"os"
	"path/filepath"
	"strings"
)

// FileSystemAbstraction is the local-disk implementation of the handful of
// operations the renderers use.
type FileSystemAbstraction struct{}

func (FileSystemAbstraction) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (FileSystemAbstraction) IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (FileSystemAbstraction) GetFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (FileSystemAbstraction) Open(path string) (*os.File, error) {
	return os.Open(path)
}

func (FileSystemAbstraction) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

func (FileSystemAbstraction) Create(path string) (*os.File, error) {
	// The renderers assume the cache folder already exists; creating it here
	// keeps a missing parent from failing every single render.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

func (FileSystemAbstraction) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (FileSystemAbstraction) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

func (FileSystemAbstraction) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (FileSystemAbstraction) Remove(path string) error { return os.Remove(path) }

func (FileSystemAbstraction) RemoveAll(path string) error { return os.RemoveAll(path) }

func (FileSystemAbstraction) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (FileSystemAbstraction) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// FileSystemHandler mirrors the fields of the arozos type the renderers read.
// RequireBuffer is always false here: the source files sit on a real mount, so
// they can be streamed rather than buffered into RAM -- which matters on an
// SBC with under 2 GB.
type FileSystemHandler struct {
	FileSystemAbstraction FileSystemAbstraction
	RequireBuffer         bool
	ReadOnly              bool
}

// LocalFileSystemHandler is the single handler instance used by this package.
func LocalFileSystemHandler() *FileSystemHandler {
	return &FileSystemHandler{}
}

// stringInArray reports whether needle is present in haystack.
// (Replaces arozos' utils.StringInArray.)
func stringInArray(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// fileExists is the package-level convenience used by the video renderer.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lowerExt returns the lower-cased extension of a path, including the dot.
func lowerExt(path string) string {
	return strings.ToLower(filepath.Ext(path))
}
