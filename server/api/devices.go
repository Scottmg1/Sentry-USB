package api

import (
	"fmt"
	"log"
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

	// Build a set of block devices that host system mount points so we
	// never offer the OS drive for selection. This handles PARTUUID-style
	// root=, NVMe, SD cards, USB-booted drives, etc.
	excludedDevs := map[string]bool{
		"mmcblk0": true, // always exclude the onboard SD card slot
	}
	systemMounts := []string{"/", "/boot", "/boot/firmware"}
	if data, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
				continue
			}
			mountpoint := fields[1]
			isSystem := false
			for _, sm := range systemMounts {
				if mountpoint == sm {
					isSystem = true
					break
				}
			}
			if !isSystem {
				continue
			}
			// Resolve symlinks (e.g. /dev/disk/by-partuuid/xxx -> /dev/sda1)
			devPath := fields[0]
			if resolved, err := filepath.EvalSymlinks(devPath); err == nil {
				devPath = resolved
			}
			dev := strings.TrimPrefix(devPath, "/dev/")
			// Strip partition suffix: mmcblk0p2 -> mmcblk0, nvme0n1p2 -> nvme0n1, sda1 -> sda
			if strings.Contains(dev, "mmcblk") || strings.Contains(dev, "nvme") || strings.Contains(dev, "loop") {
				// mmcblk/nvme/loop style: partition suffix is "pN"
				if idx := strings.LastIndex(dev, "p"); idx > 0 {
					excludedDevs[dev[:idx]] = true
				}
			} else {
				// sd-style: partition suffix is just digits
				excludedDevs[strings.TrimRight(dev, "0123456789")] = true
			}
			excludedDevs[dev] = true // also exclude the partition device name itself
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		if excludedDevs[name] {
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

// ensureMediaFolders creates media folders if they don't exist.
// Wraps and LicensePlate are always created (user-uploadable).
// Music, LightShow, and Boombox are only created if configured.
func ensureMediaFolders() {
	// Always create user-uploadable folders
	dirs := []string{
		"/mutable/Wraps",
		"/mutable/LicensePlate",
	}
	// Only create optional media folders if their backing files exist
	if fileExists("/backingfiles/music_disk.bin") {
		dirs = append(dirs, "/var/www/html/fs/Music")
	}
	if fileExists("/backingfiles/lightshow_disk.bin") {
		dirs = append(dirs, "/var/www/html/fs/LightShow")
	}
	if fileExists("/backingfiles/boombox_disk.bin") {
		dirs = append(dirs, "/var/www/html/fs/Boombox")
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			log.Printf("Warning: could not create %s: %v", d, err)
		}
	}
}
