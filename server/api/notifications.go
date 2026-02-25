package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pairing code charset — excludes ambiguous chars (0/O, 1/I)
const pairingCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const pairingCodeLength = 6
const pairingCodeExpiry = 5 * time.Minute
const maxActiveCodes = 3

// Credential storage path
const notificationCredentialsPath = "/root/.sentryusb/notification-credentials.json"

// NotificationCredentials holds the Pi's unique identity for the notification backend
type NotificationCredentials struct {
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"`
}

// PairingCode represents a pending pairing code
type PairingCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

var (
	notifCreds     *NotificationCredentials
	notifCredsOnce sync.Once
	activeCodes    []PairingCode
	codesMu        sync.Mutex
)

// getOrCreateCredentials loads or generates the Pi's notification credentials
func getOrCreateCredentials() *NotificationCredentials {
	notifCredsOnce.Do(func() {
		// Try to load existing
		data, err := os.ReadFile(notificationCredentialsPath)
		if err == nil {
			var creds NotificationCredentials
			if json.Unmarshal(data, &creds) == nil && creds.DeviceID != "" && creds.DeviceSecret != "" {
				notifCreds = &creds
				return
			}
		}

		// Generate new credentials
		deviceID := generateSecureToken(32)
		deviceSecret := generateSecureToken(64)

		notifCreds = &NotificationCredentials{
			DeviceID:     deviceID,
			DeviceSecret: deviceSecret,
		}

		// Persist to disk
		dir := filepath.Dir(notificationCredentialsPath)
		os.MkdirAll(dir, 0700)
		jsonData, _ := json.MarshalIndent(notifCreds, "", "  ")
		if err := os.WriteFile(notificationCredentialsPath, jsonData, 0600); err != nil {
			log.Printf("[notifications] Failed to save credentials: %v", err)
		} else {
			log.Printf("[notifications] Generated new device credentials: %s", notifCreds.DeviceID[:8])
		}

		// Also write to sentryusb.conf if not present
		writeCredentialsToConfig(notifCreds)
	})
	return notifCreds
}

// writeCredentialsToConfig adds MOBILE_PUSH_DEVICE_ID and MOBILE_PUSH_SECRET to the config file
func writeCredentialsToConfig(creds *NotificationCredentials) {
	configPath := findConfigFilePath()
	if configPath == "" {
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	content := string(data)

	// Check if already present
	if strings.Contains(content, "MOBILE_PUSH_DEVICE_ID") {
		return
	}

	// Append notification credentials
	addition := fmt.Sprintf("\n# Mobile push notification credentials (auto-generated)\nexport MOBILE_PUSH_DEVICE_ID='%s'\nexport MOBILE_PUSH_SECRET='%s'\n",
		creds.DeviceID, creds.DeviceSecret)

	if err := os.WriteFile(configPath, []byte(content+addition), 0600); err != nil {
		log.Printf("[notifications] Failed to write credentials to config: %v", err)
	}
}

// generateSecureToken generates a hex-encoded random token of the given byte length
func generateSecureToken(byteLen int) string {
	b := make([]byte, byteLen)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// generatePairingCode creates a random 6-char alphanumeric code
func generatePairingCode() string {
	code := make([]byte, pairingCodeLength)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(pairingCodeCharset))))
		code[i] = pairingCodeCharset[n.Int64()]
	}
	return string(code)
}

// cleanExpiredCodes removes expired pairing codes
func cleanExpiredCodes() {
	now := time.Now()
	valid := activeCodes[:0]
	for _, c := range activeCodes {
		if c.ExpiresAt.After(now) {
			valid = append(valid, c)
		}
	}
	activeCodes = valid
}

// POST /api/notifications/generate-code
func (h *handlers) generateNotificationPairingCode(w http.ResponseWriter, r *http.Request) {
	creds := getOrCreateCredentials()
	if creds == nil {
		writeError(w, http.StatusInternalServerError, "Failed to initialize notification credentials")
		return
	}

	codesMu.Lock()
	defer codesMu.Unlock()

	// Clean expired codes
	cleanExpiredCodes()

	// Check max active codes
	if len(activeCodes) >= maxActiveCodes {
		writeError(w, http.StatusTooManyRequests, "Too many active pairing codes. Wait for existing codes to expire.")
		return
	}

	code := generatePairingCode()
	expiresAt := time.Now().Add(pairingCodeExpiry)

	activeCodes = append(activeCodes, PairingCode{
		Code:      code,
		ExpiresAt: expiresAt,
	})

	// Register code with notification backend (synchronous — must succeed before returning code to user)
	if err := registerCodeWithBackend(creds, code); err != nil {
		// Remove the code we just added since registration failed
		activeCodes = activeCodes[:len(activeCodes)-1]
		log.Printf("[notifications] Failed to register code %s with backend: %v", code, err)
		writeError(w, http.StatusBadGateway, "Failed to register pairing code with notification server. Check internet connection.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"code":       code,
		"expires_at": expiresAt.Format(time.RFC3339),
	})

	log.Printf("[notifications] Generated pairing code %s (expires %s)", code, expiresAt.Format(time.Kitchen))
}

// registerCodeWithBackend sends the pairing code to the notification relay server
func registerCodeWithBackend(creds *NotificationCredentials, code string) error {
	hostname, _ := os.Hostname()
	body := fmt.Sprintf(`{"device_id":"%s","device_secret":"%s","code":"%s","hostname":"%s"}`,
		creds.DeviceID, creds.DeviceSecret, code, hostname)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", "https://notifications.sentry-six.com/register-code", strings.NewReader(body))
	if err != nil {
		log.Printf("[notifications] Failed to create register-code request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[notifications] Failed to register code with backend: %v", err)
		return fmt.Errorf("failed to reach notification server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[notifications] Backend register-code returned %d: %s", resp.StatusCode, string(respBody))
		return fmt.Errorf("backend returned %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[notifications] Code %s registered with backend successfully", code)
	return nil
}

// GET /api/notifications/paired-devices
func (h *handlers) listNotificationPairedDevices(w http.ResponseWriter, r *http.Request) {
	creds := getOrCreateCredentials()
	if creds == nil {
		writeError(w, http.StatusInternalServerError, "Notification credentials not available")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://notifications.sentry-six.com/devices?device_id=%s", creds.DeviceID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("X-Device-Secret", creds.DeviceSecret)

	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to reach notification backend")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// DELETE /api/notifications/paired-devices/{id}
func (h *handlers) removeNotificationPairedDevice(w http.ResponseWriter, r *http.Request) {
	pairingId := r.PathValue("id")
	if pairingId == "" {
		writeError(w, http.StatusBadRequest, "Missing pairing ID")
		return
	}

	creds := getOrCreateCredentials()
	if creds == nil {
		writeError(w, http.StatusInternalServerError, "Notification credentials not available")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://notifications.sentry-six.com/devices/%s?device_id=%s", pairingId, creds.DeviceID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	req.Header.Set("X-Device-Secret", creds.DeviceSecret)

	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to reach notification backend")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// POST /api/notifications/test — send a test push to all paired devices
func (h *handlers) sendTestNotification(w http.ResponseWriter, r *http.Request) {
	creds := getOrCreateCredentials()
	if creds == nil {
		writeError(w, http.StatusInternalServerError, "Notification credentials not available")
		return
	}

	hostname, _ := os.Hostname()
	payload := fmt.Sprintf(`{"device_id":"%s","device_secret":"%s","title":"SentryUSB Test","message":"Test notification from %s — push notifications are working!"}`,
		creds.DeviceID, creds.DeviceSecret, hostname)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	testReq, err := http.NewRequest("POST", "https://notifications.sentry-six.com/send", strings.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create request")
		return
	}
	testReq.Header.Set("Content-Type", "application/json")

	testResp, err := httpClient.Do(testReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to reach notification backend")
		return
	}
	defer testResp.Body.Close()

	testBody, _ := io.ReadAll(testResp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(testResp.StatusCode)
	w.Write(testBody)

	log.Printf("[notifications] Test notification sent, backend returned %d", testResp.StatusCode)
}
