package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"imuslab.com/dezkvm/dezkvmd/mod/dezkvm/storage"
	"imuslab.com/dezkvm/dezkvmd/mod/thumbnails"
)

/*
	storage_handlers.go

	Top-level HTTP handler wrappers for the file manager, storage volume and
	image-write APIs. These thin functions extract the instance UUID from the
	request and delegate to the corresponding DezkVM methods defined in
	mod/dezkvm/handlers.go.

	Routes are registered in register_ipkvm_apis (api.go).
*/

// handleStorageVolumeInfo returns disk-usage statistics for a KVM instance's
// mass storage device (total, used, free bytes + category breakdown).
func handleStorageVolumeInfo(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleStorageVolumeInfo(w, r, uuid)
}

// handleStorageFiles dispatches GET / POST / DELETE for the file manager.
//
//	GET    – list directory contents  (?path=...)
//	POST   – create file or directory (JSON body: {"path":"...","type":"file"|"dir"})
//	DELETE – delete file or directory (?path=...)
func handleStorageFiles(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	switch r.Method {
	case http.MethodGet:
		dezkvmManager.HandleFileManagerListDir(w, r, uuid)
	case http.MethodPost:
		dezkvmManager.HandleFileManagerCreate(w, r, uuid)
	case http.MethodDelete:
		dezkvmManager.HandleFileManagerDelete(w, r, uuid)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStorageUpload accepts a multipart POST upload.
// Form fields: "path" (destination directory), "files" (one or more files).
func handleStorageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleFileManagerUpload(w, r, uuid)
}

// handleStorageRename renames a file or directory.
// JSON body: {"path":"...","new_name":"..."}
func handleStorageRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleFileManagerRename(w, r, uuid)
}

// handleStorageMove moves a file or directory.
// JSON body: {"src":"...","dst":"..."}
func handleStorageMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleFileManagerMove(w, r, uuid)
}

// handleStorageCopy copies a file or directory.
// JSON body: {"src":"...","dst":"..."}
func handleStorageCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleFileManagerCopy(w, r, uuid)
}

// handleStorageDownload serves a file for download.
// Query param: path=<relative path>
func handleStorageDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleFileManagerDownload(w, r, uuid)
}

// handleStorageUnmount flushes pending writes and unmounts the mass storage
// device from the KVM host, so the drive can be switched or physically pulled
// without corrupting the filesystem.  POST only.
func handleStorageUnmount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	targetInstance, err := dezkvmManager.GetInstanceByUUID(uuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	if err := targetInstance.UnmountMassStorage(); err != nil {
		http.Error(w, "Failed to unmount: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The contents may be rewritten while the drive is away, so the cached
	// previews for this device can no longer be trusted.
	if storageUuid := targetInstance.MassStorageUUID(); storageUuid != "" && thumbRenderer != nil {
		thumbRenderer.PurgeDevice(storageUuid)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleStorageThumbnail serves a cached preview image for a file on the mass
// storage device, rendering it on first request.
//
//	GET  ?path=<relative path>
//
// Files with no available renderer answer 404 so the browser can fall back to
// its extension icon; that is an expected outcome, not an error.
func handleStorageThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	targetInstance, err := dezkvmManager.GetInstanceByUUID(uuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	mountPoint, err := targetInstance.GetMassStorageMountPoint()
	if err != nil {
		http.Error(w, "Mass storage not mounted: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Missing required query parameter: path", http.StatusBadRequest)
		return
	}

	// Resolve against the mount point.  CleanPath rejects anything that would
	// escape the drive, so the renderer never sees an unvalidated path.
	srcPath, err := storage.CleanPath(mountPoint, relPath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// The cache is keyed by the drive's own UUID rather than the instance
	// UUID, so previews survive the drive being moved to another KVM port.
	deviceID := targetInstance.MassStorageUUID()
	if deviceID == "" {
		deviceID = uuid
	}

	if thumbRenderer == nil {
		http.Error(w, "Thumbnail renderer unavailable", http.StatusServiceUnavailable)
		return
	}

	cachePath, err := thumbRenderer.Get(deviceID, relPath, srcPath)
	if err != nil {
		if errors.Is(err, thumbnails.ErrNotRenderable) || os.IsNotExist(err) {
			http.Error(w, "No preview available", http.StatusNotFound)
			return
		}
		// A render that failed for any other reason (corrupt file, another
		// request already rendering it) is also just "no preview yet".
		http.Error(w, "Preview generation failed", http.StatusNotFound)
		return
	}

	// Cached previews are immutable for a given source mtime; the browser may
	// keep them for the lifetime of the page.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, filepath.Clean(cachePath))
}

// handleStorageFormatPreview returns the partition layout that would result
// from formatting the mass storage device.  No data is written to disk.
//
//	GET  – legacy single-partition preview (query params: filesystem, label)
//	POST – multi-partition preview (JSON body: {"part_table":"...","partitions":[...]})
func handleStorageFormatPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleStorageFormatPreview(w, r, uuid)
}

// handleStorageDiskFormat formats the mass storage device.
// POST only.  JSON body: either the legacy single-partition form
// {"filesystem":"fat32|exfat|ext4|ntfs","label":"..."} or a partition table
// {"part_table":"msdos|gpt|","partitions":[{"size_mb":...,"filesystem":"...","label":"..."}]}
func handleStorageDiskFormat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")

	// Formatting replaces the filesystem, so every cached preview for this
	// drive is about to describe a file that no longer exists. Purge before
	// the format runs -- afterwards the drive carries a new PTUUID and the
	// old cache directory would just be an orphan nothing ever cleans up.
	if targetInstance, err := dezkvmManager.GetInstanceByUUID(uuid); err == nil && thumbRenderer != nil {
		if storageUuid := targetInstance.MassStorageUUID(); storageUuid != "" {
			thumbRenderer.PurgeDevice(storageUuid)
		}
	}

	dezkvmManager.HandleStorageDiskFormat(w, r, uuid)
}

// handleISOWrite starts writing a stored image from the ISO library to the
// mass storage device.  POST only.  JSON body: {"iso_name":"debian.iso"}
func handleISOWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")

	var req struct {
		ISOName string `json:"iso_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ISOName == "" {
		http.Error(w, "Missing required field: iso_name", http.StatusBadRequest)
		return
	}

	isoPath, err := isoManager.GetPath(req.ISOName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	dezkvmManager.HandleISOWriteStart(w, r, uuid, isoPath)
}

// handleISOWriteUpload streams an uploaded image directly onto the mass
// storage device (multipart POST, fields: "size" (optional) then "iso").
func handleISOWriteUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleISOWriteUpload(w, r, uuid)
}

// handleISOWriteStatus returns the progress of the current / last image write.
func handleISOWriteStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleISOWriteStatus(w, r, uuid)
}
