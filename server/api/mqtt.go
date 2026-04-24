package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/shell"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTManager handles Home Assistant MQTT integration
type MQTTManager struct {
	client          mqtt.Client
	config          MQTTConfig
	connected       bool
	mu              sync.RWMutex
	deviceID        string
	deviceName      string
	publishTicker   *time.Ticker
	stopChan        chan struct{}
	archiveProgress float64
}

// MQTTConfig holds MQTT connection settings
type MQTTConfig struct {
	Enabled   bool   `json:"enabled"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	BaseTopic string `json:"base_topic"` // e.g., "sentryusb"
}

// HomeAssistantDevice represents the device in Home Assistant
type HomeAssistantDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SwVersion    string   `json:"sw_version"`
}

// HomeAssistantEntity represents a sensor/button in Home Assistant
type HomeAssistantEntity struct {
	DeviceClass       string               `json:"device_class,omitempty"`
	Icon              string               `json:"icon,omitempty"`
	Name              string               `json:"name"`
	StateTopic        string               `json:"state_topic"`
	CommandTopic      string               `json:"command_topic,omitempty"`
	UnitOfMeasurement string               `json:"unit_of_measurement,omitempty"`
	ValueTemplate     string               `json:"value_template,omitempty"`
	PayloadOn         string               `json:"payload_on,omitempty"`
	PayloadOff        string               `json:"payload_off,omitempty"`
	Retain            bool                 `json:"retain"`
	ExpireAfter       int                  `json:"expire_after,omitempty"`
	Device            *HomeAssistantDevice `json:"device"`
	UniqueID          string               `json:"unique_id"`
	Platform          string               `json:"platform,omitempty"`
}

var mqttManager *MQTTManager
var mqttLock sync.Mutex

const mqttExpireAfterSeconds = 90

// GetMQTTManager returns the singleton MQTT manager
func GetMQTTManager() *MQTTManager {
	mqttLock.Lock()
	defer mqttLock.Unlock()
	if mqttManager == nil {
		mqttManager = &MQTTManager{
			stopChan: make(chan struct{}),
		}
	}
	return mqttManager
}

// SetMQTTArchiveProgress updates the archive progress percentage
func SetMQTTArchiveProgress(progress float64) {
	mgr := GetMQTTManager()
	mgr.mu.Lock()
	mgr.archiveProgress = progress
	mgr.mu.Unlock()
}

// InitMQTT initializes the MQTT manager with configuration
func (m *MQTTManager) Init(config MQTTConfig) error {
	m.mu.Lock()
	m.config = config
	m.mu.Unlock()

	if !config.Enabled || config.Host == "" {
		return nil
	}

	// Extract device ID and name
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		mid := strings.TrimSpace(string(data))
		if len(mid) >= 4 {
			m.deviceID = strings.ToUpper(mid[len(mid)-4:])
		}
	}
	m.deviceName = fmt.Sprintf("SentryUSB-%s", m.deviceID)

	return m.connect()
}

// connect establishes MQTT connection
func (m *MQTTManager) connect() error {
	opts := mqtt.NewClientOptions()
	brokerURL := fmt.Sprintf("tcp://%s:%d", m.config.Host, m.config.Port)
	opts.AddBroker(brokerURL)
	opts.SetClientID(fmt.Sprintf("sentryusb-%s", m.deviceID))

	if m.config.Username != "" {
		opts.SetUsername(m.config.Username)
	}
	if m.config.Password != "" {
		opts.SetPassword(m.config.Password)
	}

	opts.SetConnectTimeout(5 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetDefaultPublishHandler(m.handleMessage)
	opts.OnConnect = m.onConnect
	opts.OnDisconnect = m.onDisconnect

	m.client = mqtt.NewClient(opts)
	token := m.client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt connection timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()

	log.Printf("Connected to MQTT broker at %s", brokerURL)

	// Start the publish ticker for periodic updates
	m.startPublishTicker()

	// Subscribe to command topics
	m.subscribeToCommands()

	return nil
}

// onConnect is called when MQTT client connects
func (m *MQTTManager) onConnect(client mqtt.Client) {
	log.Println("MQTT client connected")
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()

	// Publish device discovery and initial state
	m.publishDiscovery()
	m.subscribeToCommands()
}

// onDisconnect is called when MQTT client disconnects
func (m *MQTTManager) onDisconnect(client mqtt.Client, err error) {
	log.Printf("MQTT client disconnected: %v", err)
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

// handleMessage handles incoming MQTT messages (e.g., archive commands)
func (m *MQTTManager) handleMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())

	log.Printf("MQTT message on %s: %s", topic, payload)

	// Archive command
	if strings.HasSuffix(topic, "/archive/set") {
		if payload == "trigger" || payload == "ON" {
			log.Println("Archive trigger received from Home Assistant")
			// This will be handled by the archive command endpoint
			// Just log it here; the actual archiving is triggered via HTTP endpoint
		}
	}
}

// subscribeToCommands subscribes to command topics
func (m *MQTTManager) subscribeToCommands() {
	if m.client == nil || !m.client.IsConnected() {
		return
	}

	baseTopic := m.config.BaseTopic
	if baseTopic == "" {
		baseTopic = "sentryusb"
	}

	commandTopic := fmt.Sprintf("%s/archive/set", baseTopic)
	token := m.client.Subscribe(commandTopic, 1, m.handleMessage)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to %s: %v", commandTopic, token.Error())
	}
}

// startPublishTicker starts the periodic update ticker
func (m *MQTTManager) startPublishTicker() {
	if m.publishTicker != nil {
		m.publishTicker.Stop()
	}
	m.publishTicker = time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-m.publishTicker.C:
				m.publishState()
			case <-m.stopChan:
				return
			}
		}
	}()
}

// publishDiscovery publishes Home Assistant device discovery messages
func (m *MQTTManager) publishDiscovery() {
	if m.client == nil || !m.client.IsConnected() {
		return
	}

	baseTopic := m.config.BaseTopic
	if baseTopic == "" {
		baseTopic = "sentryusb"
	}

	device := &HomeAssistantDevice{
		Identifiers:  []string{fmt.Sprintf("sentryusb-%s", m.deviceID)},
		Name:         m.deviceName,
		Manufacturer: "SentryUSB",
		Model:        getSBCModel(),
		SwVersion:    "1.0.0",
	}

	// Define all entities
	entities := map[string]HomeAssistantEntity{
		"uptime": {
			Name:              "Uptime",
			DeviceClass:       "duration",
			StateTopic:        fmt.Sprintf("%s/uptime", baseTopic),
			UnitOfMeasurement: "s",
			Icon:              "mdi:clock-outline",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_uptime", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"cpu_temp": {
			Name:              "CPU Temperature",
			DeviceClass:       "temperature",
			StateTopic:        fmt.Sprintf("%s/cpu_temp", baseTopic),
			UnitOfMeasurement: "°C",
			ValueTemplate:     "{{ (value / 1000) | round(1) }}",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_cpu_temp", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"fan_speed": {
			Name:              "Fan Speed",
			StateTopic:        fmt.Sprintf("%s/fan_speed", baseTopic),
			UnitOfMeasurement: "RPM",
			Icon:              "mdi:fan",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_fan_speed", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"usb_drives_active": {
			Name:        "USB Drives Active",
			DeviceClass: "plug",
			StateTopic:  fmt.Sprintf("%s/usb_drives_active", baseTopic),
			Icon:        "mdi:usb",
			Device:      device,
			UniqueID:    fmt.Sprintf("sentryusb_%s_usb_drives_active", m.deviceID),
			Retain:      true,
			ExpireAfter: mqttExpireAfterSeconds,
		},
		"total_space": {
			Name:              "Total Storage",
			DeviceClass:       "data_size",
			StateTopic:        fmt.Sprintf("%s/total_space", baseTopic),
			UnitOfMeasurement: "B",
			Icon:              "mdi:database",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_total_space", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"free_space": {
			Name:              "Free Storage",
			DeviceClass:       "data_size",
			StateTopic:        fmt.Sprintf("%s/free_space", baseTopic),
			UnitOfMeasurement: "B",
			Icon:              "mdi:database-check",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_free_space", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"wifi_ssid": {
			Name:        "WiFi SSID",
			StateTopic:  fmt.Sprintf("%s/wifi_ssid", baseTopic),
			Icon:        "mdi:wifi",
			Device:      device,
			UniqueID:    fmt.Sprintf("sentryusb_%s_wifi_ssid", m.deviceID),
			Retain:      true,
			ExpireAfter: mqttExpireAfterSeconds,
		},
		"wifi_strength": {
			Name:              "WiFi Strength",
			DeviceClass:       "signal_strength",
			StateTopic:        fmt.Sprintf("%s/wifi_strength", baseTopic),
			UnitOfMeasurement: "dBm",
			Icon:              "mdi:wifi-strength-2",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_wifi_strength", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"wifi_ip": {
			Name:        "WiFi IP Address",
			StateTopic:  fmt.Sprintf("%s/wifi_ip", baseTopic),
			Icon:        "mdi:ip",
			Device:      device,
			UniqueID:    fmt.Sprintf("sentryusb_%s_wifi_ip", m.deviceID),
			Retain:      true,
			ExpireAfter: mqttExpireAfterSeconds,
		},
		"ether_ip": {
			Name:        "Ethernet IP Address",
			StateTopic:  fmt.Sprintf("%s/ether_ip", baseTopic),
			Icon:        "mdi:ip",
			Device:      device,
			UniqueID:    fmt.Sprintf("sentryusb_%s_ether_ip", m.deviceID),
			Retain:      true,
			ExpireAfter: mqttExpireAfterSeconds,
		},
		"ether_speed": {
			Name:              "Ethernet Speed",
			StateTopic:        fmt.Sprintf("%s/ether_speed", baseTopic),
			UnitOfMeasurement: "Mbps",
			Icon:              "mdi:ethernet",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_ether_speed", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"archive_progress": {
			Name:              "Archive Progress",
			StateTopic:        fmt.Sprintf("%s/archive_progress", baseTopic),
			UnitOfMeasurement: "%%",
			Icon:              "mdi:archive",
			Device:            device,
			UniqueID:          fmt.Sprintf("sentryusb_%s_archive_progress", m.deviceID),
			Retain:            true,
			ExpireAfter:       mqttExpireAfterSeconds,
		},
		"archive": {
			Name:         "Trigger Archive",
			CommandTopic: fmt.Sprintf("%s/archive/set", baseTopic),
			StateTopic:   fmt.Sprintf("%s/archive_state", baseTopic),
			PayloadOn:    "trigger",
			PayloadOff:   "off",
			Icon:         "mdi:play-circle",
			Device:       device,
			UniqueID:     fmt.Sprintf("sentryusb_%s_archive_button", m.deviceID),
			Retain:       false,
		},
	}

	// Publish discovery messages for each entity
	for entityID, entity := range entities {
		// Determine component type
		componentType := "sensor"
		if entity.CommandTopic != "" {
			componentType = "button"
		}

		topic := fmt.Sprintf("homeassistant/%s/%s-%s/config", componentType, m.deviceID, entityID)
		payload, _ := json.Marshal(entity)

		token := m.client.Publish(topic, 1, true, string(payload))
		token.Wait()
		if token.Error() != nil {
			log.Printf("Failed to publish discovery for %s: %v", entityID, token.Error())
		}
	}

	log.Println("Home Assistant discovery published")
}

// publishState publishes current status to MQTT
func (m *MQTTManager) publishState() {
	if m.client == nil || !m.client.IsConnected() {
		return
	}

	baseTopic := m.config.BaseTopic
	if baseTopic == "" {
		baseTopic = "sentryusb"
	}

	// Get current status
	status := m.getStatus()

	topics := map[string]string{
		fmt.Sprintf("%s/uptime", baseTopic):            status["uptime"],
		fmt.Sprintf("%s/cpu_temp", baseTopic):          status["cpu_temp"],
		fmt.Sprintf("%s/fan_speed", baseTopic):         status["fan_speed"],
		fmt.Sprintf("%s/usb_drives_active", baseTopic): status["usb_drives_active"],
		fmt.Sprintf("%s/total_space", baseTopic):       status["total_space"],
		fmt.Sprintf("%s/free_space", baseTopic):        status["free_space"],
		fmt.Sprintf("%s/wifi_ssid", baseTopic):         status["wifi_ssid"],
		fmt.Sprintf("%s/wifi_strength", baseTopic):     status["wifi_strength"],
		fmt.Sprintf("%s/wifi_ip", baseTopic):           status["wifi_ip"],
		fmt.Sprintf("%s/ether_ip", baseTopic):          status["ether_ip"],
		fmt.Sprintf("%s/ether_speed", baseTopic):       status["ether_speed"],
		fmt.Sprintf("%s/archive_progress", baseTopic):  fmt.Sprintf("%.0f", m.archiveProgress),
		fmt.Sprintf("%s/archive_state", baseTopic):     "off",
	}

	for topic, value := range topics {
		if value != "" {
			token := m.client.Publish(topic, 1, true, value)
			token.Wait()
			if token.Error() != nil {
				log.Printf("Failed to publish to %s: %v", topic, token.Error())
			}
		}
	}
}

// getStatus retrieves current status (simplified version)
func (m *MQTTManager) getStatus() map[string]string {
	status := make(map[string]string)

	// CPU temp
	if data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		status["cpu_temp"] = strings.TrimSpace(string(data))
	}

	// Fan speed
	matches, _ := findFilesMatching("/sys/devices/platform/cooling_fan/hwmon/*/fan1_input")
	if len(matches) > 0 {
		if data, err := os.ReadFile(matches[0]); err == nil {
			status["fan_speed"] = strings.TrimSpace(string(data))
		}
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			status["uptime"] = parts[0]
		}
	}

	// USB drives active
	if _, err := os.Stat("/sys/kernel/config/usb_gadget/sentryusb"); err == nil {
		status["usb_drives_active"] = "yes"
	} else {
		status["usb_drives_active"] = "no"
	}

	// Disk space
	if out, err := shell.Run("stat", "--file-system", "--format=%b %S %f", "/backingfiles/."); err == nil {
		var blocks, blockSize, freeBlocks uint64
		fmt.Sscanf(strings.TrimSpace(out), "%d %d %d", &blocks, &blockSize, &freeBlocks)
		status["total_space"] = fmt.Sprintf("%d", blocks*blockSize)
		status["free_space"] = fmt.Sprintf("%d", freeBlocks*blockSize)
	}

	// WiFi info
	wifiDev := findNetDevice("wl*")
	if wifiDev != "" {
		if out, err := shell.Run("iwgetid", "-r", wifiDev); err == nil {
			status["wifi_ssid"] = strings.TrimSpace(out)
		}
		if out, err := shell.Run("iw", wifiDev, "link"); err == nil {
			if match := regexp.MustCompile(`signal: ([-\d]+) dBm`).FindStringSubmatch(out); len(match) > 1 {
				status["wifi_strength"] = match[1]
			}
		}
		if out, err := shell.Run("hostname", "-I"); err == nil {
			parts := strings.Fields(out)
			for i, part := range parts {
				ip := net.ParseIP(part)
				if ip != nil && ip.IsPrivate() {
					if i == 0 || !strings.HasPrefix(parts[0], "192.168.") {
						status["wifi_ip"] = part
					}
					break
				}
			}
		}
	}

	// Ethernet info
	etherDev := findNetDevice("eth*")
	if etherDev != "" {
		if out, err := shell.Run("hostname", "-I"); err == nil {
			parts := strings.Fields(out)
			if len(parts) > 0 {
				// Try to get ethernet IP (usually the second address)
				if len(parts) > 1 {
					status["ether_ip"] = parts[1]
				} else {
					status["ether_ip"] = parts[0]
				}
			}
		}
		if out, err := shell.Run("ethtool", etherDev); err == nil {
			if match := regexp.MustCompile(`Speed: (\d+)Mb/s`).FindStringSubmatch(out); len(match) > 1 {
				status["ether_speed"] = match[1]
			}
		}
	}

	// Archive progress (read from archive status file)
	m.updateArchiveProgress()

	return status
}

// findFilesMatching is a helper to find files matching a glob pattern
func findFilesMatching(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// updateArchiveProgress reads the archive status and updates the progress
func (m *MQTTManager) updateArchiveProgress() {
	data, err := os.ReadFile("/tmp/archive_status.json")
	if err != nil {
		// No archive in progress
		m.mu.Lock()
		m.archiveProgress = 0
		m.mu.Unlock()
		return
	}

	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		m.mu.Lock()
		m.archiveProgress = 0
		m.mu.Unlock()
		return
	}

	// Calculate progress as a percentage
	current, ok1 := status["current"].(float64)
	total, ok2 := status["total"].(float64)

	if ok1 && ok2 && total > 0 {
		progress := (current / total) * 100
		m.mu.Lock()
		m.archiveProgress = progress
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		m.archiveProgress = 0
		m.mu.Unlock()
	}
}

// Close closes the MQTT connection
func (m *MQTTManager) Close() {
	close(m.stopChan)
	if m.publishTicker != nil {
		m.publishTicker.Stop()
	}
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect(100)
	}
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

// IsConnected returns whether the MQTT manager is connected
func (m *MQTTManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected && m.client != nil && m.client.IsConnected()
}
