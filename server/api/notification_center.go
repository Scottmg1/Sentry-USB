package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const notificationHistoryPath = "/mutable/sentryusb-notifications.json"
const maxNotificationHistory = 500

// NotificationEvent represents a single logged notification event
type NotificationEvent struct {
	ID        string            `json:"id"`
	Timestamp int64             `json:"ts"`
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Providers []string          `json:"providers"`
	Results   map[string]string `json:"results"`
}

// NotificationSettings holds per-type toggle states
type NotificationSettings struct {
	ArchiveStart    bool `json:"archive_start"`
	ArchiveComplete bool `json:"archive_complete"`
	ArchiveError    bool `json:"archive_error"`
	Temperature     bool `json:"temperature"`
	KeepAwake       bool `json:"keep_awake_failure"`
	Update          bool `json:"update"`
	Drives          bool `json:"drives"`
	RTCBattery      bool `json:"rtc_battery"`
	MusicSync       bool `json:"music_sync"`
}

var historyMu sync.RWMutex

func defaultNotificationSettings() NotificationSettings {
	return NotificationSettings{
		ArchiveStart:    true,
		ArchiveComplete: true,
		ArchiveError:    true,
		Temperature:     true,
		KeepAwake:       true,
		Update:          true,
		Drives:          true,
		RTCBattery:      true,
		MusicSync:       true,
	}
}

// loadNotificationSettings reads notification type toggles from preferences
func loadNotificationSettings() NotificationSettings {
	prefs := loadPreferences()
	settings := defaultNotificationSettings()

	boolPref := func(key string, def bool) bool {
		v, ok := prefs[key]
		if !ok {
			return def
		}
		return v == "true"
	}

	settings.ArchiveStart = boolPref("notify_archive_start", true)
	settings.ArchiveComplete = boolPref("notify_archive_complete", true)
	settings.ArchiveError = boolPref("notify_archive_error", true)
	settings.Temperature = boolPref("notify_temperature", true)
	settings.KeepAwake = boolPref("notify_keep_awake_failure", true)
	settings.Update = boolPref("notify_update", true)
	settings.Drives = boolPref("notify_drives", true)
	settings.RTCBattery = boolPref("notify_rtc_battery", true)
	settings.MusicSync = boolPref("notify_music_sync", true)

	return settings
}

// saveNotificationSettings persists notification type toggles into preferences
func saveNotificationSettings(s NotificationSettings) error {
	prefs := loadPreferences()

	set := func(key string, val bool) {
		if val {
			prefs[key] = "true"
		} else {
			prefs[key] = "false"
		}
	}

	set("notify_archive_start", s.ArchiveStart)
	set("notify_archive_complete", s.ArchiveComplete)
	set("notify_archive_error", s.ArchiveError)
	set("notify_temperature", s.Temperature)
	set("notify_keep_awake_failure", s.KeepAwake)
	set("notify_update", s.Update)
	set("notify_drives", s.Drives)
	set("notify_rtc_battery", s.RTCBattery)
	set("notify_music_sync", s.MusicSync)

	return savePreferences(prefs)
}

// loadNotificationHistory reads the notification history log
func loadNotificationHistory() []NotificationEvent {
	historyMu.RLock()
	defer historyMu.RUnlock()

	data, err := os.ReadFile(notificationHistoryPath)
	if err != nil {
		return []NotificationEvent{}
	}
	var events []NotificationEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return []NotificationEvent{}
	}
	return events
}

// saveNotificationHistory writes the notification history log (caller must hold historyMu)
func saveNotificationHistory(events []NotificationEvent) error {
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(notificationHistoryPath, data, 0644)
}

// GET /api/notifications/settings
func (h *handlers) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	settings := loadNotificationSettings()
	writeJSON(w, http.StatusOK, settings)
}

// PUT /api/notifications/settings
func (h *handlers) updateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	var settings NotificationSettings
	if err := json.Unmarshal(body, &settings); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := saveNotificationSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save notification settings")
		return
	}

	writeOK(w)
}

// GET /api/notifications/history?limit=50&offset=0&type=archive_start
func (h *handlers) getNotificationHistory(w http.ResponseWriter, r *http.Request) {
	events := loadNotificationHistory()

	// Filter by type if specified
	typeFilter := r.URL.Query().Get("type")
	if typeFilter != "" {
		filtered := make([]NotificationEvent, 0)
		for _, e := range events {
			if e.Type == typeFilter {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	total := len(events)

	// Parse pagination
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	// Events are stored newest-first; apply pagination
	if offset >= len(events) {
		events = []NotificationEvent{}
	} else {
		end := offset + limit
		if end > len(events) {
			end = len(events)
		}
		events = events[offset:end]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// DELETE /api/notifications/history — clear all history
func (h *handlers) clearNotificationHistory(w http.ResponseWriter, r *http.Request) {
	historyMu.Lock()
	defer historyMu.Unlock()

	if err := saveNotificationHistory([]NotificationEvent{}); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to clear notification history")
		return
	}
	writeOK(w)
}

// DELETE /api/notifications/history/{id} — delete a single notification
func (h *handlers) deleteNotificationHistoryItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing notification ID")
		return
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	data, err := os.ReadFile(notificationHistoryPath)
	if err != nil {
		writeOK(w)
		return
	}
	var events []NotificationEvent
	if err := json.Unmarshal(data, &events); err != nil {
		writeOK(w)
		return
	}

	filtered := make([]NotificationEvent, 0, len(events))
	for _, e := range events {
		if e.ID != id {
			filtered = append(filtered, e)
		}
	}

	if err := saveNotificationHistory(filtered); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save notification history")
		return
	}
	writeOK(w)
}

// AppendNotificationHistory adds a new event to the history log (called by send-push-message via API)
// POST /api/notifications/history
func (h *handlers) appendNotificationHistory(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	var event NotificationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid event data")
		return
	}

	// Set timestamp if not provided
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	// Generate ID if not provided
	if event.ID == "" {
		event.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	// Load existing history
	data, _ := os.ReadFile(notificationHistoryPath)
	var events []NotificationEvent
	json.Unmarshal(data, &events)

	// Prepend new event (newest first)
	events = append([]NotificationEvent{event}, events...)

	// Trim to max size
	if len(events) > maxNotificationHistory {
		events = events[:maxNotificationHistory]
	}

	if err := saveNotificationHistory(events); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save notification history")
		return
	}

	writeJSON(w, http.StatusOK, event)
}

// GET /api/notifications/settings/check?type=archive_start — quick check if a notification type is enabled
func (h *handlers) checkNotificationType(w http.ResponseWriter, r *http.Request) {
	notifType := r.URL.Query().Get("type")
	if notifType == "" {
		writeError(w, http.StatusBadRequest, "Missing type parameter")
		return
	}

	settings := loadNotificationSettings()
	enabled := true

	switch notifType {
	case "archive_start":
		enabled = settings.ArchiveStart
	case "archive_complete":
		enabled = settings.ArchiveComplete
	case "archive_error":
		enabled = settings.ArchiveError
	case "temperature":
		enabled = settings.Temperature
	case "keep_awake_failure":
		enabled = settings.KeepAwake
	case "update":
		enabled = settings.Update
	case "drives":
		enabled = settings.Drives
	case "rtc_battery":
		enabled = settings.RTCBattery
	case "music_sync":
		enabled = settings.MusicSync
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"type":    notifType,
		"enabled": enabled,
	})
}
