package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// HealthCheck result types
type checkStatus string

const (
	statusPass checkStatus = "pass"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

type healthItem struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail,omitempty"`
}

type healthCategory struct {
	Name  string       `json:"name"`
	Items []healthItem `json:"items"`
}

type healthReport struct {
	Summary    string           `json:"summary"`
	Categories []healthCategory `json:"categories"`
}

func (h *handlers) healthCheck(w http.ResponseWriter, r *http.Request) {
	var categories []healthCategory

	categories = append(categories, checkCoreFiles())
	categories = append(categories, checkConfig())
	categories = append(categories, checkDiskImages())
	categories = append(categories, checkMounts())
	categories = append(categories, checkServices())
	categories = append(categories, checkUSBGadget())
	categories = append(categories, checkNetwork())
	categories = append(categories, checkBLE())

	// Build summary
	var pass, warn, fail int
	for _, cat := range categories {
		for _, item := range cat.Items {
			switch item.Status {
			case statusPass:
				pass++
			case statusWarn:
				warn++
			case statusFail:
				fail++
			}
		}
	}
	summary := fmt.Sprintf("%d passed, %d warnings, %d failed", pass, warn, fail)

	writeJSON(w, http.StatusOK, healthReport{
		Summary:    summary,
		Categories: categories,
	})
}

// --- Check functions ---

func checkCoreFiles() healthCategory {
	items := []healthItem{}

	coreFiles := []struct {
		path string
		name string
		exec bool
	}{
		{"/opt/sentryusb/sentryusb", "SentryUSB binary", true},
		{"/root/bin/archiveloop", "archiveloop script", false},
		{"/root/bin/envsetup.sh", "envsetup.sh", false},
		{"/root/bin/enable_gadget.sh", "enable_gadget.sh", true},
		{"/root/bin/disable_gadget.sh", "disable_gadget.sh", true},
		{"/root/bin/make_snapshot.sh", "make_snapshot.sh", true},
		{"/root/bin/release_snapshot.sh", "release_snapshot.sh", true},
		{"/root/bin/manage_free_space.sh", "manage_free_space.sh", true},
		{"/root/bin/waitforidle", "waitforidle", false},
		{"/root/bin/mountimage", "mountimage", false},
		{"/root/bin/remountfs_rw", "remountfs_rw", false},
		{"/sbin/mount.sentryusb", "mount.sentryusb symlink", false},
	}

	for _, f := range coreFiles {
		info, err := os.Lstat(f.path)
		if err != nil {
			items = append(items, healthItem{f.name, statusFail, f.path + " missing"})
			continue
		}
		if f.exec && info.Mode()&0111 == 0 {
			items = append(items, healthItem{f.name, statusWarn, f.path + " exists but not executable"})
			continue
		}
		items = append(items, healthItem{f.name, statusPass, f.path})
	}

	return healthCategory{Name: "Core Files", Items: items}
}

func checkConfig() healthCategory {
	items := []healthItem{}

	// Config file
	configPath := findConfigFilePath()
	if configPath == "" {
		items = append(items, healthItem{"Config file", statusFail, "No sentryusb.conf found"})
	} else {
		items = append(items, healthItem{"Config file", statusPass, configPath})
	}

	// Setup finished marker
	finished := false
	for _, p := range setupFinishedPaths {
		if _, err := os.Stat(p); err == nil {
			items = append(items, healthItem{"Setup finished", statusPass, p + " exists"})
			finished = true
			break
		}
	}
	if !finished {
		items = append(items, healthItem{"Setup finished", statusFail, "SENTRYUSB_SETUP_FINISHED marker not found"})
	}

	// fstab entries
	fstab, err := os.ReadFile("/etc/fstab")
	if err != nil {
		items = append(items, healthItem{"fstab", statusFail, "Cannot read /etc/fstab"})
	} else {
		fstabStr := string(fstab)
		for _, entry := range []struct {
			label string
			name  string
		}{
			{"backingfiles", "backingfiles in fstab"},
			{"mutable", "mutable in fstab"},
		} {
			if strings.Contains(fstabStr, entry.label) {
				items = append(items, healthItem{entry.name, statusPass, "Present"})
			} else {
				items = append(items, healthItem{entry.name, statusFail, "Missing from /etc/fstab"})
			}
		}
		if strings.Contains(fstabStr, "cam_disk.bin") {
			items = append(items, healthItem{"cam_disk in fstab", statusPass, "Present"})
		} else {
			items = append(items, healthItem{"cam_disk in fstab", statusWarn, "Missing (no cam disk configured?)"})
		}
	}

	return healthCategory{Name: "Configuration", Items: items}
}

func checkDiskImages() healthCategory {
	items := []healthItem{}

	images := []struct {
		path     string
		name     string
		required bool
	}{
		{"/backingfiles/cam_disk.bin", "cam_disk.bin", true},
		{"/backingfiles/music_disk.bin", "music_disk.bin", false},
		{"/backingfiles/lightshow_disk.bin", "lightshow_disk.bin", false},
		{"/backingfiles/boombox_disk.bin", "boombox_disk.bin", false},
	}

	for _, img := range images {
		info, err := os.Stat(img.path)
		if err != nil {
			if img.required {
				items = append(items, healthItem{img.name, statusFail, "Missing"})
			} else {
				items = append(items, healthItem{img.name, statusPass, "Not configured (optional)"})
			}
			continue
		}
		sizeMB := info.Size() / (1024 * 1024)
		items = append(items, healthItem{img.name, statusPass, fmt.Sprintf("%d MB", sizeMB)})
	}

	// Snapshots directory
	if info, err := os.Stat("/backingfiles/snapshots"); err == nil && info.IsDir() {
		entries, _ := os.ReadDir("/backingfiles/snapshots")
		count := 0
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "snap-") {
				count++
			}
		}
		items = append(items, healthItem{"Snapshots", statusPass, fmt.Sprintf("%d snapshots", count)})
	} else {
		items = append(items, healthItem{"Snapshots", statusWarn, "No snapshots directory"})
	}

	return healthCategory{Name: "Disk Images", Items: items}
}

func checkMounts() healthCategory {
	items := []healthItem{}

	mounts, _ := os.ReadFile("/proc/mounts")
	mountStr := string(mounts)

	for _, mp := range []struct {
		path string
		name string
	}{
		{"/backingfiles", "backingfiles"},
		{"/mutable", "mutable"},
	} {
		if strings.Contains(mountStr, " "+mp.path+" ") {
			items = append(items, healthItem{mp.name, statusPass, "Mounted"})
		} else {
			items = append(items, healthItem{mp.name, statusFail, "Not mounted"})
		}
	}

	// TeslaCam directory
	if info, err := os.Stat("/mutable/TeslaCam"); err == nil && info.IsDir() {
		items = append(items, healthItem{"TeslaCam directory", statusPass, "/mutable/TeslaCam exists"})
	} else {
		items = append(items, healthItem{"TeslaCam directory", statusWarn, "/mutable/TeslaCam missing"})
	}

	return healthCategory{Name: "Mount Points", Items: items}
}

func checkServices() healthCategory {
	items := []healthItem{}

	services := []struct {
		name     string
		display  string
		required bool
	}{
		{"sentryusb", "SentryUSB web server", true},
		{"sentryusb-archive", "Archive loop", true},
		{"autofs", "autofs (snapshot mounts)", true},
		{"bluetooth", "Bluetooth", false},
		{"avahi-daemon", "mDNS (avahi)", false},
	}

	for _, svc := range services {
		out, err := exec.Command("systemctl", "is-active", svc.name).Output()
		state := strings.TrimSpace(string(out))
		if err == nil && state == "active" {
			items = append(items, healthItem{svc.display, statusPass, "Active"})
		} else {
			if svc.required {
				items = append(items, healthItem{svc.display, statusFail, "Not active (" + state + ")"})
			} else {
				items = append(items, healthItem{svc.display, statusWarn, "Not active (" + state + ")"})
			}
		}
	}

	return healthCategory{Name: "Services", Items: items}
}

func checkUSBGadget() healthCategory {
	items := []healthItem{}

	gadgetRoot := "/sys/kernel/config/usb_gadget/sentryusb"
	if _, err := os.Stat(gadgetRoot); err != nil {
		items = append(items, healthItem{"USB gadget", statusWarn, "Not configured (drives disconnected from host?)"})
		return healthCategory{Name: "USB Gadget", Items: items}
	}
	items = append(items, healthItem{"USB gadget directory", statusPass, "Exists"})

	// Check UDC
	udc, err := os.ReadFile(gadgetRoot + "/UDC")
	if err != nil || strings.TrimSpace(string(udc)) == "" {
		items = append(items, healthItem{"UDC (USB controller)", statusFail, "Not bound to a USB controller"})
	} else {
		items = append(items, healthItem{"UDC (USB controller)", statusPass, strings.TrimSpace(string(udc))})
	}

	// Check LUNs
	for i := 0; i < 4; i++ {
		lunPath := fmt.Sprintf("%s/functions/mass_storage.0/lun.%d/file", gadgetRoot, i)
		data, err := os.ReadFile(lunPath)
		if err != nil {
			break
		}
		file := strings.TrimSpace(string(data))
		if file == "" {
			continue
		}
		lunName := fmt.Sprintf("LUN %d", i)
		if _, err := os.Stat(file); err != nil {
			items = append(items, healthItem{lunName, statusFail, file + " (file missing!)"})
		} else {
			// Extract friendly name from path
			base := strings.TrimSuffix(strings.TrimPrefix(file, "/backingfiles/"), "_disk.bin")
			items = append(items, healthItem{lunName, statusPass, base + " → " + file})
		}
	}

	return healthCategory{Name: "USB Gadget", Items: items}
}

func checkNetwork() healthCategory {
	items := []healthItem{}

	// Check wlan0 via nmcli
	out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,STATE", "device", "status").Output()
	if err != nil {
		items = append(items, healthItem{"NetworkManager", statusFail, "nmcli not available"})
		return healthCategory{Name: "Network", Items: items}
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dev, state := parts[0], parts[1]
		if dev == "wlan0" {
			if state == "connected" {
				items = append(items, healthItem{"wlan0 (WiFi)", statusPass, "Connected"})
			} else {
				items = append(items, healthItem{"wlan0 (WiFi)", statusWarn, "State: " + state})
			}
		}
		if dev == "ap0" {
			if state == "connected" {
				items = append(items, healthItem{"ap0 (Access Point)", statusPass, "Active"})
			} else {
				items = append(items, healthItem{"ap0 (Access Point)", statusWarn, "State: " + state})
			}
		}
	}

	// Check AP connection exists
	apOut, _ := exec.Command("nmcli", "-t", "con", "show", "SENTRYUSB_AP").Output()
	if len(apOut) > 0 {
		items = append(items, healthItem{"AP connection config", statusPass, "SENTRYUSB_AP defined"})
	}

	// Check NM dispatcher script for AP recreation
	if _, err := os.Stat("/etc/NetworkManager/dispatcher.d/90-sentryusb-ap"); err == nil {
		items = append(items, healthItem{"AP dispatcher script", statusPass, "Present"})
	} else {
		// Only warn if AP is configured
		if len(apOut) > 0 {
			items = append(items, healthItem{"AP dispatcher script", statusWarn, "Missing (ap0 may not survive reboot)"})
		}
	}

	return healthCategory{Name: "Network", Items: items}
}

func checkBLE() healthCategory {
	items := []healthItem{}

	// Check if BLE is configured
	vin := readBLEVin()
	if vin == "" {
		items = append(items, healthItem{"BLE configuration", statusPass, "Not configured (skipping BLE checks)"})
		return healthCategory{Name: "Bluetooth / BLE", Items: items}
	}
	items = append(items, healthItem{"TESLA_BLE_VIN", statusPass, vin})

	// BLE binaries
	for _, bin := range []struct {
		path string
		name string
	}{
		{"/root/bin/tesla-control", "tesla-control"},
		{"/root/bin/tesla-keygen", "tesla-keygen"},
	} {
		if info, err := os.Stat(bin.path); err != nil {
			items = append(items, healthItem{bin.name, statusFail, "Missing"})
		} else if info.Mode()&0111 == 0 {
			items = append(items, healthItem{bin.name, statusWarn, "Exists but not executable"})
		} else {
			items = append(items, healthItem{bin.name, statusPass, "Present"})
		}
	}

	// BLE keys
	if _, err := os.Stat("/root/.ble/key_public.pem"); err != nil {
		items = append(items, healthItem{"BLE public key", statusFail, "Missing — keys not generated"})
	} else {
		items = append(items, healthItem{"BLE public key", statusPass, "Present"})
	}
	if _, err := os.Stat("/root/.ble/key_private.pem"); err != nil {
		items = append(items, healthItem{"BLE private key", statusFail, "Missing — keys not generated"})
	} else {
		items = append(items, healthItem{"BLE private key", statusPass, "Present"})
	}

	// bluez package
	out, _ := exec.Command("dpkg-query", "-W", "--showformat=${db:Status-Status}", "bluez").Output()
	if strings.TrimSpace(string(out)) == "installed" {
		items = append(items, healthItem{"bluez package", statusPass, "Installed"})
	} else {
		items = append(items, healthItem{"bluez package", statusFail, "Not installed"})
	}

	return healthCategory{Name: "Bluetooth / BLE", Items: items}
}
