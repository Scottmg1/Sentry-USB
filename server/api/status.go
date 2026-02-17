package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Scottmg1/Sentry-USB/server/shell"
)

type piStatus struct {
	CPUTemp        string `json:"cpu_temp"`
	NumSnapshots   string `json:"num_snapshots"`
	SnapshotOldest string `json:"snapshot_oldest"`
	SnapshotNewest string `json:"snapshot_newest"`
	TotalSpace     string `json:"total_space"`
	FreeSpace      string `json:"free_space"`
	Uptime         string `json:"uptime"`
	DrivesActive   string `json:"drives_active"`
	WifiSSID       string `json:"wifi_ssid"`
	WifiFreq       string `json:"wifi_freq"`
	WifiStrength   string `json:"wifi_strength"`
	WifiIP         string `json:"wifi_ip"`
	EtherIP        string `json:"ether_ip"`
	EtherSpeed     string `json:"ether_speed"`
}

func (h *handlers) getStatus(w http.ResponseWriter, r *http.Request) {
	status := piStatus{}

	// CPU temperature
	if data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		status.CPUTemp = strings.TrimSpace(string(data))
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			status.Uptime = parts[0]
		}
	}

	// USB gadget status
	if _, err := os.Stat("/sys/kernel/config/usb_gadget/teslausb"); err == nil {
		status.DrivesActive = "yes"
	} else {
		status.DrivesActive = "no"
	}

	// Snapshots
	snapshots := findSnapshots()
	status.NumSnapshots = fmt.Sprintf("%d", len(snapshots))
	if len(snapshots) > 0 {
		if info, err := os.Stat(snapshots[0]); err == nil {
			status.SnapshotOldest = fmt.Sprintf("%d", info.ModTime().Unix())
		}
		if info, err := os.Stat(snapshots[len(snapshots)-1]); err == nil {
			status.SnapshotNewest = fmt.Sprintf("%d", info.ModTime().Unix())
		}
	}

	// Disk space
	if out, err := shell.Run("stat", "--file-system", "--format=%b %S %f", "/backingfiles/."); err == nil {
		var blocks, blockSize, freeBlocks uint64
		fmt.Sscanf(strings.TrimSpace(out), "%d %d %d", &blocks, &blockSize, &freeBlocks)
		status.TotalSpace = fmt.Sprintf("%d", blocks*blockSize)
		status.FreeSpace = fmt.Sprintf("%d", freeBlocks*blockSize)
	}

	// WiFi info
	wifiDev := findNetDevice("wl*")
	if wifiDev != "" {
		if out, err := shell.Run("iwgetid", "-r", wifiDev); err == nil {
			status.WifiSSID = strings.TrimSpace(out)
		}
		if out, err := shell.Run("iwgetid", "-r", "-f", wifiDev); err == nil {
			status.WifiFreq = strings.TrimSpace(out)
		}
		if out, err := shell.Run("iwconfig", wifiDev); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "Link Quality") {
					parts := strings.Split(line, "Link Quality=")
					if len(parts) > 1 {
						qual := strings.Fields(parts[1])
						if len(qual) > 0 {
							status.WifiStrength = qual[0]
						}
					}
				}
			}
		}
		if out, err := shell.Run("ip", "-4", "addr", "show", wifiDev); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "inet ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						status.WifiIP = strings.Split(parts[1], "/")[0]
					}
				}
			}
		}
	}

	// Ethernet info
	ethDev := findNetDevice("eth*")
	if ethDev == "" {
		ethDev = findNetDevice("en*")
	}
	if ethDev != "" {
		if out, err := shell.Run("ip", "-4", "addr", "show", ethDev); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "inet ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						status.EtherIP = strings.Split(parts[1], "/")[0]
					}
				}
			}
		}
		if out, err := shell.Run("ethtool", ethDev); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "Speed:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						status.EtherSpeed = strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, status)
}

func findSnapshots() []string {
	var snapshots []string
	_ = filepath.Walk("/backingfiles/snapshots/", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "snap.bin" {
			snapshots = append(snapshots, path)
		}
		return nil
	})
	sort.Strings(snapshots)
	return snapshots
}

func findNetDevice(pattern string) string {
	matches, err := filepath.Glob("/sys/class/net/" + pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return filepath.Base(matches[0])
}

type piConfig struct {
	HasCam      string `json:"has_cam"`
	HasMusic    string `json:"has_music"`
	HasLightshow string `json:"has_lightshow"`
	HasBoombox  string `json:"has_boombox"`
	UsesBLE     string `json:"uses_ble"`
}

func (h *handlers) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := piConfig{
		HasCam:      boolToYesNo(fileExists("/backingfiles/cam_disk.bin")),
		HasMusic:    boolToYesNo(fileExists("/backingfiles/music_disk.bin")),
		HasLightshow: boolToYesNo(fileExists("/backingfiles/lightshow_disk.bin")),
		HasBoombox:  boolToYesNo(fileExists("/backingfiles/boombox_disk.bin")),
	}

	// Check if BLE is configured
	configPath := findConfigFilePath()
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			content := string(data)
			for _, line := range strings.Split(content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "export TESLA_BLE_VIN=") {
					cfg.UsesBLE = "yes"
					break
				}
			}
		}
	}
	if cfg.UsesBLE == "" {
		cfg.UsesBLE = "no"
	}

	writeJSON(w, http.StatusOK, cfg)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func findConfigFilePath() string {
	paths := []string{
		"/root/teslausb_setup_variables.conf",
		"/boot/firmware/teslausb_setup_variables.conf",
		"/boot/teslausb_setup_variables.conf",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

