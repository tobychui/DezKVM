package storage

/*
	filemanager.go

	Provides file-system operations (list, create, upload, delete, rename, move,
	copy, download) scoped to a root directory. All user-supplied paths are
	sanitised by CleanPath to prevent path-traversal attacks.
*/

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileEntry represents a single entry returned by ListDir.
type FileEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	Mode    string    `json:"mode"`
}

// CleanPath resolves and validates a user-supplied relative path against rootDir
// to prevent path-traversal attacks. It returns the absolute path only when it
// is guaranteed to remain inside rootDir; otherwise it returns an error.
func CleanPath(rootDir, relPath string) (string, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}

	// Convert any forward-slash separators and clean the joined path.
	joined := filepath.Join(root, filepath.FromSlash(relPath))
	clean := filepath.Clean(joined)

	// Require the resolved path to equal root or be directly under it.
	// Adding the separator prevents "/root-extra" from matching "/root".
	if clean != root && !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return "", errors.New("path traversal detected: path is outside root directory")
	}

	return clean, nil
}

// ListDir returns the directory entries at relPath inside rootDir.
func ListDir(rootDir, relPath string) ([]FileEntry, error) {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, err
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, FileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: info.ModTime(),
			Mode:    info.Mode().String(),
		})
	}
	return result, nil
}

// CreateFile creates an empty file at relPath inside rootDir, creating any
// intermediate directories. Returns an error if the file already exists.
func CreateFile(rootDir, relPath string) error {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0750); err != nil {
		return err
	}
	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	return f.Close()
}

// CreateDir creates the directory (and any needed parents) at relPath inside rootDir.
func CreateDir(rootDir, relPath string) error {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(absPath, 0750)
}

// Delete removes the file or directory at relPath inside rootDir.
// Directory removal is recursive. The root directory itself cannot be deleted.
func Delete(rootDir, relPath string) error {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	root, _ := filepath.Abs(rootDir)
	if absPath == root {
		return errors.New("cannot delete root directory")
	}
	return os.RemoveAll(absPath)
}

// Rename renames the entry at relPath to newName (bare filename, no separators).
func Rename(rootDir, relPath, newName string) error {
	if strings.ContainsAny(newName, `/\`) {
		return errors.New("new name must not contain path separators")
	}
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	newAbs := filepath.Join(filepath.Dir(absPath), newName)
	// Validate the renamed path still resides inside root.
	root, _ := filepath.Abs(rootDir)
	if newAbs != root && !strings.HasPrefix(newAbs, root+string(filepath.Separator)) {
		return errors.New("path traversal detected in new name")
	}
	return os.Rename(absPath, newAbs)
}

// Move moves the entry at srcRelPath to dstRelPath inside rootDir,
// creating any intermediate destination directories.
func Move(rootDir, srcRelPath, dstRelPath string) error {
	srcAbs, err := CleanPath(rootDir, srcRelPath)
	if err != nil {
		return err
	}
	dstAbs, err := CleanPath(rootDir, dstRelPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0750); err != nil {
		return err
	}
	return os.Rename(srcAbs, dstAbs)
}

// Copy copies the file or directory tree at srcRelPath to dstRelPath inside rootDir.
func Copy(rootDir, srcRelPath, dstRelPath string) error {
	srcAbs, err := CleanPath(rootDir, srcRelPath)
	if err != nil {
		return err
	}
	dstAbs, err := CleanPath(rootDir, dstRelPath)
	if err != nil {
		return err
	}
	return copyPath(srcAbs, dstAbs)
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPath(
			filepath.Join(src, e.Name()),
			filepath.Join(dst, e.Name()),
		); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// UploadFile writes the content from fh to relPath inside rootDir, creating any
// intermediate directories. The caller is responsible for closing fh.
func UploadFile(rootDir, relPath string, fh multipart.File) error {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0750); err != nil {
		return err
	}
	out, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, fh)
	return err
}

// ServeFile writes the file at relPath to w as an HTTP download response.
// Returns an error if the path is a directory or cannot be opened.
func ServeFile(rootDir, relPath string, w http.ResponseWriter, r *http.Request) error {
	absPath, err := CleanPath(rootDir, relPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("cannot download a directory")
	}
	f, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer f.Close()

	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return nil
}
