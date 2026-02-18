package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listBlockDevices returns removable/external block devices suitable for DATA_DRIVE
func (h *handlers) listBlockDevices(w http.ResponseWriter, r *http.Request) {
	type blockDev struct {
		Path   string `json:"path"`
		Name   string `json:"name"`
		SizeGB string `json:"size_gb"`
		Model  string `json:"model"`
	}

	var devices []blockDev

	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		writeJSON(w, http.StatusOK, devices)
		return
	}

	// Find the root device so we can exclude it
	rootDev := ""
	if data, err := os.ReadFile("/proc/cmdline"); err == nil {
		for _, part := range strings.Fields(string(data)) {
			if strings.HasPrefix(part, "root=") {
				rootDev = strings.TrimPrefix(part, "root=")
				rootDev = strings.TrimPrefix(rootDev, "/dev/")
				// Strip partition suffix: mmcblk0p2 -> mmcblk0
				if idx := strings.LastIndex(rootDev, "p"); idx > 0 {
					rootDev = rootDev[:idx]
				}
				break
			}
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		if name == rootDev {
			continue
		}

		devPath := "/dev/" + name

		// Read size (in 512-byte sectors)
		sizeStr := ""
		if data, err := os.ReadFile(filepath.Join("/sys/block", name, "size")); err == nil {
			sectors, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			gb := float64(sectors*512) / (1024 * 1024 * 1024)
			if gb < 0.5 {
				continue
			}
			sizeStr = fmt.Sprintf("%.1f", gb)
		}

		model := ""
		if data, err := os.ReadFile(filepath.Join("/sys/block", name, "device", "model")); err == nil {
			model = strings.TrimSpace(string(data))
		}

		label := name
		if model != "" {
			label = name + " (" + model + ")"
		}
		if sizeStr != "" {
			label += " - " + sizeStr + " GB"
		}

		devices = append(devices, blockDev{
			Path:   devPath,
			Name:   label,
			SizeGB: sizeStr,
			Model:  model,
		})
	}

	writeJSON(w, http.StatusOK, devices)
}

// ensureMediaFolders creates Wraps and LicensePlate folders if they don't exist
func ensureMediaFolders() {
	dirs := []string{
		"/var/www/html/fs/Wraps",
		"/var/www/html/fs/LicensePlate",
	}
	for _, d := range dirs {
		os.MkdirAll(d, 0755)
	}
}
