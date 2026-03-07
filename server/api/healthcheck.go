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
		{"/backingfiles/wraps_disk.bin", "wraps_disk.bin", false},
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
	for i := 0; i < 5; i++ {
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
	if _, err := os.Stat("/etc/NetworkManager/dispatcher.d/10-sentryusb-ap"); err == nil {
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

	// --- Pi hardware ---
	if model, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		items = append(items, healthItem{"Pi model", statusPass, strings.TrimRight(string(model), "\x00\n")})
	}
	if kernel, err := exec.Command("uname", "-r").Output(); err == nil {
		items = append(items, healthItem{"Kernel", statusPass, strings.TrimSpace(string(kernel))})
	}

	// --- BlueZ / bluetoothd ---
	bluezVer, _ := exec.Command("bluetoothctl", "--version").Output()
	if v := strings.TrimSpace(string(bluezVer)); v != "" {
		items = append(items, healthItem{"BlueZ version", statusPass, v})
	} else {
		items = append(items, healthItem{"BlueZ version", statusFail, "bluetoothctl not found"})
	}

	btdActive, _ := exec.Command("systemctl", "is-active", "bluetooth").Output()
	btdState := strings.TrimSpace(string(btdActive))
	if btdState == "active" {
		items = append(items, healthItem{"bluetooth.service", statusPass, "Active"})
	} else {
		items = append(items, healthItem{"bluetooth.service", statusFail, "State: " + btdState})
	}

	// Check --experimental flag
	btdCmd, _ := exec.Command("bash", "-c", "ps -eo args | grep bluetoothd | grep -v grep").Output()
	btdCmdStr := strings.TrimSpace(string(btdCmd))
	if strings.Contains(btdCmdStr, "--experimental") {
		items = append(items, healthItem{"--experimental flag", statusPass, "Active"})
	} else {
		items = append(items, healthItem{"--experimental flag", statusFail, "NOT set — required for GATT peripheral"})
	}

	// Check experimental conf drop-in
	if _, err := os.Stat("/etc/systemd/system/bluetooth.service.d/sentryusb-experimental.conf"); err == nil {
		items = append(items, healthItem{"bluetoothd drop-in", statusPass, "sentryusb-experimental.conf present"})
	} else {
		items = append(items, healthItem{"bluetoothd drop-in", statusFail, "sentryusb-experimental.conf missing"})
	}

	// --- BLE Adapter ---
	hciOut, _ := exec.Command("hciconfig", "hci0").Output()
	hciStr := string(hciOut)
	if strings.Contains(hciStr, "UP RUNNING") {
		items = append(items, healthItem{"HCI adapter (hci0)", statusPass, "UP RUNNING"})
	} else if strings.Contains(hciStr, "DOWN") {
		items = append(items, healthItem{"HCI adapter (hci0)", statusFail, "DOWN"})
	} else if len(hciStr) == 0 {
		items = append(items, healthItem{"HCI adapter (hci0)", statusFail, "Not found"})
	} else {
		items = append(items, healthItem{"HCI adapter (hci0)", statusWarn, "Unknown state"})
	}

	// Adapter address
	for _, line := range strings.Split(hciStr, "\n") {
		if strings.Contains(line, "BD Address") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "Address:" && i+1 < len(parts) {
					items = append(items, healthItem{"Adapter address", statusPass, parts[i+1]})
					break
				}
			}
			break
		}
	}

	// --- sentryusb-ble daemon ---
	bleActive, _ := exec.Command("systemctl", "is-active", "sentryusb-ble").Output()
	bleState := strings.TrimSpace(string(bleActive))
	if bleState == "active" {
		items = append(items, healthItem{"sentryusb-ble.service", statusPass, "Active"})
	} else {
		items = append(items, healthItem{"sentryusb-ble.service", statusFail, "State: " + bleState})
	}

	// Daemon uptime
	bleUptime, _ := exec.Command("bash", "-c",
		"systemctl show sentryusb-ble --property=ActiveEnterTimestamp --value").Output()
	if ts := strings.TrimSpace(string(bleUptime)); ts != "" {
		items = append(items, healthItem{"BLE daemon started", statusPass, ts})
	}

	// BLE daemon script exists
	if _, err := os.Stat("/root/bin/sentryusb-ble.py"); err == nil {
		items = append(items, healthItem{"sentryusb-ble.py", statusPass, "Present"})
	} else {
		items = append(items, healthItem{"sentryusb-ble.py", statusFail, "Missing"})
	}

	// --- D-Bus ---
	if _, err := os.Stat("/etc/dbus-1/system.d/com.sentryusb.ble.conf"); err == nil {
		items = append(items, healthItem{"D-Bus policy file", statusPass, "com.sentryusb.ble.conf present"})
	} else {
		items = append(items, healthItem{"D-Bus policy file", statusWarn, "Missing — may cause GATT issues on Pi 5/Bookworm"})
	}

	// Check if bus name is claimed
	busOut, _ := exec.Command("busctl", "status", "com.sentryusb.ble").Output()
	if len(busOut) > 0 && !strings.Contains(string(busOut), "not found") {
		items = append(items, healthItem{"D-Bus bus name", statusPass, "com.sentryusb.ble claimed"})
	} else {
		items = append(items, healthItem{"D-Bus bus name", statusWarn, "com.sentryusb.ble not claimed"})
	}

	// --- GATT & Advertisement (from daemon journal) ---
	// Use -b (current boot) instead of --since today so that one-time
	// startup messages (GATT registration, advertisement) remain visible
	// even after midnight.
	journalOut, _ := exec.Command("journalctl", "-u", "sentryusb-ble",
		"-b", "--no-pager", "-q", "--output=cat").Output()
	journal := string(journalOut)

	if strings.Contains(journal, "GATT application registered") {
		items = append(items, healthItem{"GATT registration", statusPass, "Registered"})
	} else {
		items = append(items, healthItem{"GATT registration", statusFail, "No 'GATT application registered' in logs"})
	}

	if strings.Contains(journal, "GATT self-test") {
		// Find the last self-test line
		for _, line := range reverseLines(journal) {
			if strings.Contains(line, "GATT self-test") {
				if strings.Contains(line, "FAILED") {
					items = append(items, healthItem{"GATT self-test", statusFail, line})
				} else {
					items = append(items, healthItem{"GATT self-test", statusPass, line})
				}
				break
			}
		}
	} else {
		items = append(items, healthItem{"GATT self-test", statusWarn, "No self-test result in logs (older daemon?)"})
	}

	if strings.Contains(journal, "Advertisement registered") {
		items = append(items, healthItem{"BLE advertisement", statusPass, "Registered"})
	} else {
		items = append(items, healthItem{"BLE advertisement", statusFail, "No 'Advertisement registered' in logs"})
	}

	if strings.Contains(journal, "GetManagedObjects called") {
		// Count calls
		count := strings.Count(journal, "GetManagedObjects called")
		items = append(items, healthItem{"GetManagedObjects calls", statusPass, fmt.Sprintf("Called %d time(s) since boot", count)})
	} else {
		items = append(items, healthItem{"GetManagedObjects calls", statusWarn, "No calls logged (BlueZ may not be querying GATT)"})
	}

	// Recent errors
	errorCount := 0
	var lastError string
	for _, line := range strings.Split(journal, "\n") {
		if strings.Contains(line, "ERROR") {
			errorCount++
			lastError = line
		}
	}
	if errorCount == 0 {
		items = append(items, healthItem{"Recent BLE errors", statusPass, "None since boot"})
	} else {
		detail := fmt.Sprintf("%d error(s) since boot", errorCount)
		if lastError != "" {
			// Truncate to last 100 chars
			if len(lastError) > 100 {
				lastError = lastError[len(lastError)-100:]
			}
			detail += " — last: " + lastError
		}
		items = append(items, healthItem{"Recent BLE errors", statusWarn, detail})
	}

	// --- BLE PIN / claiming ---
	if _, err := os.Stat("/root/.sentryusb/ble-pin"); err == nil {
		items = append(items, healthItem{"BLE PIN (claimed)", statusPass, "Device is claimed"})
	} else {
		items = append(items, healthItem{"BLE PIN (claimed)", statusPass, "Unclaimed (first-time setup)"})
	}

	// --- Tesla BLE (optional) ---
	vin := readBLEVin()
	if vin != "" {
		items = append(items, healthItem{"TESLA_BLE_VIN", statusPass, vin})
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
	}

	return healthCategory{Name: "Bluetooth / BLE", Items: items}
}

// reverseLines splits text into lines and returns them in reverse order.
func reverseLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}
