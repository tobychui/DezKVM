package thumbnails

/*
	renderer.go

	Entry point of the thumbnail package. Replaces arozos' metadata.go, which
	dispatched through a virtual filesystem and served results as base64 over a
	websocket. DezKVM renders straight to a cache file on disk and serves that
	file, so this dispatcher is deliberately much smaller.

	Layout of the cache, rooted at the directory passed to NewRenderHandler:

	    thumb/{device_id}/{relative_path_of_the_file}.jpg

	Mirroring the source tree keeps invalidation trivial (delete the matching
	sub-tree) and means two devices never collide on the same file name.
*/

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNotRenderable is returned for files this package has no renderer for.
// Callers should treat it as "show the file-type icon instead", not a failure.
var ErrNotRenderable = errors.New("no thumbnail renderer for this file type")

// Extensions handled by each renderer. Keep these in sync with the
// thumbable types in www/js/file-icons.js -- the browser only asks for
// previews of files it believes are renderable.
var (
	audioFormats = []string{".mp3", ".ogg", ".flac"}
	imageFormats = []string{".png", ".jpeg", ".jpg", ".webp"}
	videoFormats = []string{".mkv", ".mp4", ".webm", ".ogv", ".avi", ".rmvb"}
	gcodeFormats = []string{".gcode", ".gco"}
	// RawImageFormats are the RAW camera formats whose embedded JPEG preview
	// can be extracted without a full demosaic.
	RawImageFormats = []string{".arw", ".cr2", ".dng", ".nef", ".raf", ".orf"}
)

// IsRawImageFile reports whether the path names a supported RAW image.
func IsRawImageFile(filePath string) bool {
	return stringInArray(RawImageFormats, lowerExt(filePath))
}

// IsRenderable reports whether a thumbnail can be produced for this path.
// It only looks at the extension, so a positive answer is not a guarantee the
// render will succeed.
func IsRenderable(path string) bool {
	ext := lowerExt(path)
	if stringInArray(audioFormats, ext) || stringInArray(imageFormats, ext) ||
		stringInArray(videoFormats, ext) || stringInArray(gcodeFormats, ext) ||
		stringInArray(RawImageFormats, ext) {
		return true
	}
	return ext == ".psd" || ext == ".svg"
}

// RenderHandler owns the thumbnail cache directory and serialises concurrent
// requests for the same file.
type RenderHandler struct {
	cacheRoot      string
	fsh            *FileSystemHandler
	renderingFiles sync.Map // absolute source path -> struct{}
}

// NewRenderHandler creates a handler caching under cacheRoot (e.g. "./thumb").
func NewRenderHandler(cacheRoot string) *RenderHandler {
	return &RenderHandler{
		cacheRoot: filepath.Clean(cacheRoot),
		fsh:       LocalFileSystemHandler(),
	}
}

// CachePath returns the location of the cached thumbnail for a file, without
// checking whether it exists. deviceID namespaces the cache per KVM port and
// relPath is the file's path relative to that device's mount point.
func (rh *RenderHandler) CachePath(deviceID string, relPath string) string {
	rel := filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(relPath), "/"))
	return filepath.Join(rh.cacheRoot, deviceID, rel+".jpg")
}

// Get returns the path to a usable thumbnail for srcPath, rendering one first
// when the cache is missing or stale. It returns ErrNotRenderable when the
// file type has no renderer.
//
// srcPath must already be resolved and validated by the caller -- this package
// does no path-traversal checking of its own.
func (rh *RenderHandler) Get(deviceID string, relPath string, srcPath string) (string, error) {
	if !IsRenderable(srcPath) {
		return "", ErrNotRenderable
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return "", err
	}
	if srcInfo.IsDir() {
		return "", ErrNotRenderable
	}

	cachePath := rh.CachePath(deviceID, relPath)

	// A cache entry older than the file it describes is stale. Comparing
	// mtimes is enough here: the drive is only writable through this daemon
	// or by the remote computer, and the latter always ends with a re-mount.
	if info, err := os.Stat(cachePath); err == nil && !info.ModTime().Before(srcInfo.ModTime()) {
		return cachePath, nil
	}

	// Collapse duplicate work: a grid scrolling past the same file twice, or
	// two browsers on the same device, must not start two decodes.
	if _, busy := rh.renderingFiles.LoadOrStore(srcPath, struct{}{}); busy {
		return "", errors.New("thumbnail is already being generated")
	}
	defer rh.renderingFiles.Delete(srcPath)

	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return "", err
	}

	// The upstream renderers write to cacheFolder + base(file) + ".jpg", so
	// handing them the cache file's own directory lands the output exactly on
	// cachePath.
	cacheFolder := filepath.Dir(cachePath) + string(filepath.Separator)
	if err := rh.render(cacheFolder, srcPath); err != nil {
		return "", err
	}
	if _, err := os.Stat(cachePath); err != nil {
		return "", errors.New("thumbnail generation produced no output")
	}
	return cachePath, nil
}

// render dispatches to the renderer matching the file's extension.
func (rh *RenderHandler) render(cacheFolder string, srcPath string) error {
	ext := lowerExt(srcPath)
	var err error

	switch {
	case stringInArray(audioFormats, ext):
		// Cover art embedded in the tags; absent art is a normal outcome.
		_, err = generateThumbnailForAudio(rh.fsh, cacheFolder, srcPath, true)
	case stringInArray(imageFormats, ext):
		_, err = generateThumbnailForImage(rh.fsh, cacheFolder, srcPath, true)
	case IsRawImageFile(srcPath):
		_, err = generateThumbnailForRAW(rh.fsh, cacheFolder, srcPath, true)
	case stringInArray(videoFormats, ext):
		_, err = generateThumbnailForVideo(rh.fsh, cacheFolder, srcPath, true)
	case stringInArray(gcodeFormats, ext):
		_, err = generateThumbnailForGcode(rh.fsh, cacheFolder, srcPath, true)
	case ext == ".psd":
		_, err = generateThumbnailForPSD(rh.fsh, cacheFolder, srcPath, true)
	case ext == ".svg":
		_, err = generateThumbnailForSVG(rh.fsh, cacheFolder, srcPath, true)
	default:
		return ErrNotRenderable
	}
	return err
}

// PurgeDevice drops every cached thumbnail for a device. Called after the disk
// is reformatted or the drive is handed back from the remote computer, where
// the contents may have changed underneath us.
func (rh *RenderHandler) PurgeDevice(deviceID string) error {
	if deviceID == "" {
		return errors.New("no device id")
	}
	return os.RemoveAll(filepath.Join(rh.cacheRoot, deviceID))
}

// getImageAsBase64 reads a generated thumbnail back as a data URI. Only used
// when a renderer is called with generateOnly = false; DezKVM serves the cache
// file directly, but the renderers still reference it.
func getImageAsBase64(fsh *FileSystemHandler, rpath string) (string, error) {
	content, err := fsh.FileSystemAbstraction.ReadFile(rpath)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(content), nil
}
