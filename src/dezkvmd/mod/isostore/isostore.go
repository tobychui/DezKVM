package isostore

/*
	isostore.go

	Host-side ISO / disk-image library for DezKVM.  Uploaded images are stored
	flat inside a single directory on the KVM host so they can be re-used later
	(e.g. written to a KVM port's USB mass-storage device for a remote OS
	reinstall).  Only .iso and .img files are accepted.

	The Manager also exposes the HTTP handlers used by the /api/v1/iso/*
	endpoints; thin wrappers in the main package register the routes.
*/

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// uploadTempSuffix marks partially uploaded files; they are hidden from List
// and cleaned up on startup.
const uploadTempSuffix = ".part"

// Entry describes one stored image file.
type Entry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// Manager provides access to the ISO storage directory.
type Manager struct {
	Dir string // absolute or relative path of the storage directory
}

// NewManager creates the storage directory when missing and removes stale
// temporary upload files left over from a previous run.
func NewManager(dir string) (*Manager, error) {
	if dir == "" {
		return nil, errors.New("iso store directory not specified")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create iso store directory %s: %w", dir, err)
	}
	m := &Manager{Dir: dir}

	// Remove leftover partial uploads.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), uploadTempSuffix) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return m, nil
}

// SanitizeName validates a user-supplied image file name.  The name must be a
// bare file name (no path separators) with a .iso or .img extension.
func SanitizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("empty file name")
	}
	if strings.ContainsAny(name, "/\\") || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return "", errors.New("invalid file name")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".iso" && ext != ".img" {
		return "", errors.New("only .iso and .img files are allowed")
	}
	return name, nil
}

// GetPath returns the absolute path of a stored image, verifying that the
// file exists.
func (m *Manager) GetPath(name string) (string, error) {
	clean, err := SanitizeName(name)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(m.Dir, clean)
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("image %s not found", clean)
	}
	if info.IsDir() {
		return "", fmt.Errorf("image %s not found", clean)
	}
	return fullPath, nil
}

// List returns all stored images sorted by name.
func (m *Manager) List() ([]Entry, error) {
	dirEntries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		if _, err := SanitizeName(e.Name()); err != nil {
			continue // skip temp files and foreign file types
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, Entry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// SaveStream stores the image content from r under name.  The data is written
// to a temporary file first and renamed into place on success, so a failed or
// aborted upload never leaves a truncated image in the library.
func (m *Manager) SaveStream(name string, r io.Reader) (int64, error) {
	clean, err := SanitizeName(name)
	if err != nil {
		return 0, err
	}
	finalPath := filepath.Join(m.Dir, clean)
	tempPath := finalPath + uploadTempSuffix

	out, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(out, r)
	if err != nil {
		out.Close()
		os.Remove(tempPath)
		return written, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tempPath)
		return written, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tempPath)
		return written, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return written, err
	}
	return written, nil
}

// Delete removes a stored image.
func (m *Manager) Delete(name string) error {
	fullPath, err := m.GetPath(name)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

// Rename renames a stored image; the new name must keep a valid extension.
func (m *Manager) Rename(oldName, newName string) error {
	oldPath, err := m.GetPath(oldName)
	if err != nil {
		return err
	}
	cleanNew, err := SanitizeName(newName)
	if err != nil {
		return err
	}
	newPath := filepath.Join(m.Dir, cleanNew)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("an image named %s already exists", cleanNew)
	}
	return os.Rename(oldPath, newPath)
}

// SpaceInfo returns the free and total bytes of the filesystem holding the
// image library.
func (m *Manager) SpaceInfo() (free uint64, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(m.Dir, &stat); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stat.Bsize)
	return stat.Bavail * blockSize, stat.Blocks * blockSize, nil
}

/*
	HTTP handlers
*/

// HandleList responds with the stored images plus library disk-space info.
// GET only.
func (m *Manager) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := m.List()
	if err != nil {
		http.Error(w, "Failed to list images: "+err.Error(), http.StatusInternalServerError)
		return
	}
	free, total, _ := m.SpaceInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"isos":    entries,
		"free_b":  free,
		"total_b": total,
	})
}

// HandleUpload accepts a multipart POST upload of one or more image files
// under the form field "files".  The request body is streamed straight to the
// library directory, so image size is only limited by the available disk
// space, not by RAM.
func (m *Manager) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Failed to parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}

	uploaded := []string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "Failed to read upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		if _, err := m.SaveStream(name, part); err != nil {
			part.Close()
			http.Error(w, "Failed to save "+name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		part.Close()
		uploaded = append(uploaded, name)
	}

	if len(uploaded) == 0 {
		http.Error(w, "No image files provided (only .iso and .img are accepted)", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"uploaded": uploaded,
	})
}

// HandleDelete removes the image given by the "name" query parameter.
// DELETE only.
func (m *Manager) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Missing name parameter", http.StatusBadRequest)
		return
	}
	if err := m.Delete(name); err != nil {
		http.Error(w, "Failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleRename renames an image.  POST with JSON body {"name":"...","new_name":"..."}.
func (m *Manager) HandleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name    string `json:"name"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.NewName == "" {
		http.Error(w, "Missing name or new_name", http.StatusBadRequest)
		return
	}
	if err := m.Rename(req.Name, req.NewName); err != nil {
		http.Error(w, "Failed to rename: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleDownload serves the image given by the "name" query parameter as an
// HTTP download.  GET only.
func (m *Manager) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Missing name parameter", http.StatusBadRequest)
		return
	}
	fullPath, err := m.GetPath(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	f, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "Failed to open image: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "Failed to stat image: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fullPath)))
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
