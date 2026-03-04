package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

type fileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type fileListResponse struct {
	Path    string      `json:"path"`
	Entries []fileEntry `json:"entries"`
}

// Allowed base paths for file operations (security)
var allowedBases = []string{
	"/mutable/TeslaCam",
	"/mutable/Wraps",
	"/mutable/LicensePlate",
	"/var/www/html/fs/Music",
	"/var/www/html/fs/LightShow",
	"/var/www/html/fs/Boombox",
}

func isPathAllowed(reqPath string) (string, bool) {
	clean := filepath.Clean(reqPath)
	for _, base := range allowedBases {
		if strings.HasPrefix(clean, base) || clean == base {
			return clean, true
		}
	}
	return clean, false
}

func (h *handlers) listFiles(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}

	// Map relative paths to allowed bases
	fullPath := reqPath
	if !filepath.IsAbs(reqPath) {
		// Try each allowed base
		found := false
		for _, base := range allowedBases {
			test := filepath.Join(base, reqPath)
			if _, err := os.Stat(test); err == nil {
				fullPath = test
				found = true
				break
			}
		}
		if !found {
			fullPath = filepath.Join(allowedBases[0], reqPath)
		}
	}

	cleanPath, allowed := isPathAllowed(fullPath)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Auto-create the directory if it's one of our allowed bases
	for _, base := range allowedBases {
		if cleanPath == base {
			os.MkdirAll(cleanPath, 0755)
			break
		}
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		// If directory doesn't exist (e.g. gadget not mounted), return empty list
		writeJSON(w, http.StatusOK, fileListResponse{Path: reqPath, Entries: []fileEntry{}})
		return
	}

	var files []fileEntry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			Name:    e.Name(),
			Path:    filepath.Join(reqPath, e.Name()),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02T15:04:05Z"),
		})
	}

	if files == nil {
		files = []fileEntry{}
	}

	writeJSON(w, http.StatusOK, fileListResponse{Path: reqPath, Entries: files})
}

func (h *handlers) createDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	cleanPath, allowed := isPathAllowed(req.Path)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	if err := os.MkdirAll(cleanPath, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
		return
	}

	writeOK(w)
}

func (h *handlers) moveFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
		Dest   string `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	srcClean, srcAllowed := isPathAllowed(req.Source)
	dstClean, dstAllowed := isPathAllowed(req.Dest)
	if !srcAllowed || !dstAllowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	if err := os.Rename(srcClean, dstClean); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to move: "+err.Error())
		return
	}

	writeOK(w)
}

func (h *handlers) copyFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
		Dest   string `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	srcClean, srcAllowed := isPathAllowed(req.Source)
	dstClean, dstAllowed := isPathAllowed(req.Dest)
	if !srcAllowed || !dstAllowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	srcFile, err := os.Open(srcClean)
	if err != nil {
		writeError(w, http.StatusNotFound, "Source not found")
		return
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstClean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create destination")
		return
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to copy: "+err.Error())
		return
	}

	writeOK(w)
}

func (h *handlers) deleteFile(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		writeError(w, http.StatusBadRequest, "Missing path parameter")
		return
	}

	cleanPath, allowed := isPathAllowed(reqPath)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Don't allow deleting the base directories themselves
	for _, base := range allowedBases {
		if cleanPath == base {
			writeError(w, http.StatusForbidden, "Cannot delete root directory")
			return
		}
	}

	if err := os.RemoveAll(cleanPath); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete: "+err.Error())
		return
	}

	// If deleted path was under SavedClips or SentryClips, clean up
	// matching symlinks in snapshot directories so those clips won't
	// be re-synced on next archive. RecentClips are left untouched.
	if strings.Contains(cleanPath, "/SavedClips/") || strings.Contains(cleanPath, "/SentryClips/") {
		go cleanupSnapshotSymlinks(cleanPath)
	}

	writeOK(w)
}

func (h *handlers) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(500 << 20) // 500MB max

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing file in upload")
		return
	}
	defer file.Close()

	destDir := r.FormValue("path")
	if destDir == "" {
		writeError(w, http.StatusBadRequest, "Missing path parameter")
		return
	}

	destPath := filepath.Join(destDir, header.Filename)
	cleanPath, allowed := isPathAllowed(destPath)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Ensure parent directory exists (e.g. user uploading to Wraps/LicensePlate for first time)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
		return
	}

	dst, err := os.Create(cleanPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create file: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to write file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"name": header.Filename,
		"path": destPath,
		"size": fmt.Sprintf("%d", header.Size),
	})
}

func (h *handlers) downloadFile(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		writeError(w, http.StatusBadRequest, "Missing path parameter")
		return
	}

	cleanPath, allowed := isPathAllowed(reqPath)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}

	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "Cannot download directory (use download-zip)")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(cleanPath)))
	http.ServeFile(w, r, cleanPath)
}

func (h *handlers) downloadZip(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		writeError(w, http.StatusBadRequest, "Missing path parameter")
		return
	}

	cleanPath, allowed := isPathAllowed(reqPath)
	if !allowed {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Use zip command to create archive
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, filepath.Base(cleanPath)))

	// Stream zip output directly to response
	output, err := shell.Run("zip", "-r", "-", cleanPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create zip")
		return
	}

	w.Write([]byte(output))
}

// cleanupSnapshotSymlinks removes symlinks in snapshot directories that
// correspond to a deleted SavedClips or SentryClips path. This prevents
// deleted clips from being re-archived on the next sync.
//
// The snapshot layout is:
//
//	/backingfiles/snapshots/snap-NNNNNN/mnt/TeslaCam/{SavedClips,SentryClips}/<event>/<file>.mp4
//
// and the mutable layout is:
//
//	/mutable/TeslaCam/{SavedClips,SentryClips}/<event>/<file>.mp4
//
// Both contain symlinks pointing into the snapshot mount.
func cleanupSnapshotSymlinks(deletedPath string) {
	// Determine the clip type and event folder name from the deleted path.
	// Expected patterns:
	//   /mutable/TeslaCam/SavedClips/<event>
	//   /mutable/TeslaCam/SentryClips/<event>
	//   /mutable/TeslaCam/SavedClips/<event>/<file>
	var clipType, eventName string
	for _, ct := range []string{"SavedClips", "SentryClips"} {
		marker := "/" + ct + "/"
		idx := strings.Index(deletedPath, marker)
		if idx >= 0 {
			clipType = ct
			rest := deletedPath[idx+len(marker):]
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) > 0 {
				eventName = parts[0]
			}
			break
		}
	}

	if clipType == "" || eventName == "" {
		return
	}

	log.Printf("[files] Cleaning up snapshot symlinks for %s/%s", clipType, eventName)

	// Walk all snapshot directories looking for matching event folders
	snapshotsBase := "/backingfiles/snapshots"
	entries, err := os.ReadDir(snapshotsBase)
	if err != nil {
		return
	}

	for _, snapEntry := range entries {
		if !snapEntry.IsDir() || !strings.HasPrefix(snapEntry.Name(), "snap-") {
			continue
		}

		// Check for the event folder in this snapshot's clip type directory
		eventDir := filepath.Join(snapshotsBase, snapEntry.Name(), "mnt", "TeslaCam", clipType, eventName)
		if _, err := os.Stat(eventDir); err != nil {
			continue
		}

		// Remove all symlinks in this event directory
		clipEntries, err := os.ReadDir(eventDir)
		if err != nil {
			continue
		}
		for _, ce := range clipEntries {
			linkPath := filepath.Join(eventDir, ce.Name())
			fi, err := os.Lstat(linkPath)
			if err != nil {
				continue
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				os.Remove(linkPath)
			}
		}

		// Remove the event directory if now empty
		remaining, _ := os.ReadDir(eventDir)
		if len(remaining) == 0 {
			os.Remove(eventDir)
		}
	}

	// Also clean up broken symlinks in /mutable/TeslaCam/<clipType>/<eventName>
	mutableEventDir := filepath.Join("/mutable/TeslaCam", clipType, eventName)
	if entries, err := os.ReadDir(mutableEventDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(mutableEventDir, e.Name())
			fi, err := os.Lstat(linkPath)
			if err != nil {
				continue
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				// Check if target still exists
				if _, err := os.Stat(linkPath); err != nil {
					os.Remove(linkPath)
				}
			}
		}
		remaining, _ := os.ReadDir(mutableEventDir)
		if len(remaining) == 0 {
			os.Remove(mutableEventDir)
		}
	}

	log.Printf("[files] Snapshot symlink cleanup complete for %s/%s", clipType, eventName)
}
