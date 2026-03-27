package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const wrapsDir = "/mutable/Wraps"

var validWrapCode = regexp.MustCompile(`^[A-Za-z0-9]{3,10}$`)

// GET /api/wraps/library — proxy browse request to support server
func (h *handlers) communityWrapsLibrary(w http.ResponseWriter, r *http.Request) {
	query := r.URL.RawQuery
	path := "/wraps/library"
	if query != "" {
		path += "?" + query
	}

	// Forward X-Passcode header if present (for admin fingerprint access)
	var headers map[string]string
	if passcode := r.Header.Get("X-Passcode"); passcode != "" {
		headers = map[string]string{"X-Passcode": passcode}
	}

	var respBody []byte
	var status int
	var err error
	if headers != nil {
		respBody, status, err = supportProxyWithHeaders("GET", path, nil, headers, 15*time.Second)
	} else {
		respBody, status, err = supportProxy("GET", path, nil, "", 15*time.Second)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// GET /api/wraps/thumbnail/{code} — proxy thumbnail image from support server
func (h *handlers) communityWrapsThumbnail(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validWrapCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(supportServerURL + "/wraps/thumbnail/" + code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch thumbnail")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.Copy(w, resp.Body)
}

// GET /api/wraps/godot/{file} — proxy Godot WebAssembly build files from support server
// NOTE: The Godot iframe now loads directly from api.sentry-six.com to avoid
// proxying the 283MB .pck through the Pi. This endpoint is kept as a fallback.
func (h *handlers) communityWrapsGodotAsset(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	// Whitelist: only allow Godot build files
	matched, _ := regexp.MatchString(`^index\.(js|wasm|pck|html)$`, file)
	if !matched {
		writeError(w, http.StatusBadRequest, "Invalid file")
		return
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(supportServerURL + "/wraps/godot/" + file)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch Godot asset")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	mimeTypes := map[string]string{
		".js":   "application/javascript",
		".wasm": "application/wasm",
		".pck":  "application/octet-stream",
		".html": "text/html",
	}
	ext := filepath.Ext(file)
	if ct, ok := mimeTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	// HTML gets short cache (may be updated); binary assets get long immutable cache
	if ext == ".html" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	}
	io.Copy(w, resp.Body)
}

// GET /api/wraps/preview/{code} — proxy 3D preview image from support server
func (h *handlers) communityWrapsPreview(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validWrapCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(supportServerURL + "/wraps/preview/" + code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch preview")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.Copy(w, resp.Body)
}

// POST /api/wraps/upload — proxy wrap upload to support server with fingerprint injection
func (h *handlers) communityWrapsUpload(w http.ResponseWriter, r *http.Request) {
	// Limit to 4MB to allow for wrap image + optional 3D preview + multipart overhead
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)

	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Failed to parse upload")
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing image file")
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	teslaModel := r.FormValue("tesla_model")

	if name == "" || teslaModel == "" {
		writeError(w, http.StatusBadRequest, "Missing name or tesla_model")
		return
	}

	// Read image file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read file")
		return
	}

	// Read optional 3D preview file
	var previewData []byte
	var previewFilename string
	previewFile, previewHeader, previewErr := r.FormFile("preview")
	if previewErr == nil {
		defer previewFile.Close()
		previewData, _ = io.ReadAll(previewFile)
		previewFilename = previewHeader.Filename
	}

	// Build new multipart request to support server
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("image", header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create multipart")
		return
	}
	part.Write(fileData)

	writer.WriteField("name", name)
	writer.WriteField("tesla_model", teslaModel)

	// Include 3D preview if provided
	if len(previewData) > 0 {
		previewPart, err := writer.CreateFormFile("preview", previewFilename)
		if err == nil {
			previewPart.Write(previewData)
		}
	}

	writer.Close()

	req, err := http.NewRequest("POST", supportServerURL+"/wraps/upload", &buf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Fingerprint", getFingerprint())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// POST /api/wraps/download/{code} — download wrap from support server and save to Pi's Wraps folder
func (h *handlers) communityWrapsDownload(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validWrapCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	// Fetch the wrap from support server
	req, err := http.NewRequest("GET", supportServerURL+"/wraps/download/"+code, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("X-Fingerprint", getFingerprint())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Read the PNG data
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to download wrap")
		return
	}

	// Determine filename from X-Wrap-Name header or Content-Disposition
	wrapName := resp.Header.Get("X-Wrap-Name")
	if wrapName == "" {
		wrapName = code
	}
	// Sanitize filename
	wrapName = sanitizeFilename(wrapName)
	if !strings.HasSuffix(strings.ToLower(wrapName), ".png") {
		wrapName += ".png"
	}

	// Ensure the wraps directory exists
	if err := os.MkdirAll(wrapsDir, 0755); err != nil {
		log.Printf("[WRAPS] Failed to create wraps directory: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to prepare wraps directory")
		return
	}

	// Write to wraps folder
	destPath := filepath.Join(wrapsDir, wrapName)

	// If file already exists, add a suffix
	if _, err := os.Stat(destPath); err == nil {
		base := strings.TrimSuffix(wrapName, ".png")
		for i := 1; i < 100; i++ {
			candidate := filepath.Join(wrapsDir, fmt.Sprintf("%s_%d.png", base, i))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				destPath = candidate
				break
			}
		}
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		log.Printf("[WRAPS] Failed to write wrap to disk: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save wrap")
		return
	}

	log.Printf("[WRAPS] Community wrap saved: %s (%d bytes)", destPath, len(data))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"filename": filepath.Base(destPath),
		"path":     destPath,
		"size":     len(data),
	})
}

// sanitizeFilename removes characters unsafe for filesystem paths
func sanitizeFilename(name string) string {
	// Keep only alphanumeric, spaces, hyphens, underscores
	safe := regexp.MustCompile(`[^a-zA-Z0-9 \-_.]`)
	result := safe.ReplaceAllString(name, "")
	if result == "" || result == ".png" {
		return "wrap"
	}
	return result
}

// POST /api/wraps/admin/validate — proxy admin passcode validation
func (h *handlers) communityWrapsAdminValidate(w http.ResponseWriter, r *http.Request) {
	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("POST", "/wraps/admin/validate", nil, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// PUT /api/wraps/admin/edit/{code} — proxy admin wrap edit
func (h *handlers) communityWrapsAdminEdit(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validWrapCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("PUT", "/wraps/admin/edit/"+code, body, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// DELETE /api/wraps/admin/delete/{code} — proxy admin wrap deletion
func (h *handlers) communityWrapsAdminDelete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validWrapCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "Invalid code")
		return
	}

	passcode := r.Header.Get("X-Passcode")
	if passcode == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	headers := map[string]string{"X-Passcode": passcode}
	respBody, status, err := supportProxyWithHeaders("DELETE", "/wraps/admin/delete/"+code, nil, headers, 15*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Community wraps service unreachable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

// communityWrapsMetadata is a helper for parsing library responses
type communityWrapsResponse struct {
	Wraps []json.RawMessage `json:"wraps"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
}
