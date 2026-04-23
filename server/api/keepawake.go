package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Indirection for tests: swap these to avoid shelling out to awake_start / awake_stop.
var (
	startKeepAwakeFn = startKeepAwake
	stopKeepAwakeFn  = stopKeepAwake

	// expirationTickInterval drives both the expire check and the
	// busy→idle re-arm detection. 5s keeps the handoff gap (archive /
	// processing ending → webui nudge coming back) short enough that
	// the car's 300s nudge cadence never notices.
	expirationTickInterval = 5 * time.Second

	// idleCheckInterval polls isBusy() while a webui request is queued
	// waiting for archive / processing / migration to finish. Poll
	// cadence doesn't affect correctness — 30s keeps the cost low.
	idleCheckInterval = 30 * time.Second
)

// KeepAwakeState represents the current state of the webui keep-awake manager.
type KeepAwakeState string

const (
	KeepAwakeIdle    KeepAwakeState = "idle"
	KeepAwakePending KeepAwakeState = "pending"
	KeepAwakeActive  KeepAwakeState = "active"
)

// KeepAwakeManager manages user-initiated keep-awake requests from the web UI.
// It supports two modes: "manual" (user picks a duration) and "auto" (heartbeat-based).
// It queues requests when archiving/processing is busy, starting keep-awake
// only after they finish.
type KeepAwakeManager struct {
	mu        sync.RWMutex
	state     KeepAwakeState
	mode      string    // "manual" or "auto"
	expiresAt time.Time // when keep-awake will stop (for countdown)
	startedAt time.Time

	// isBusy returns true if archiving or drive processing is in progress.
	isBusy func() bool

	// pendingDuration stores the requested duration when queued (manual mode).
	pendingDuration time.Duration

	stopCh chan struct{}
	doneCh chan struct{}
}

// keepAwakeReasonLabel returns a human-readable reason for nudge log lines.
func keepAwakeReasonLabel(mode string) string {
	switch mode {
	case "manual":
		return "Manual"
	case "auto":
		return "Auto Keep Awake"
	default:
		return "Keep Awake"
	}
}

// NewKeepAwakeManager creates a new manager with a function to check if the
// system is busy (archiving or processing drives).
func NewKeepAwakeManager(isBusy func() bool) *KeepAwakeManager {
	return &KeepAwakeManager{
		state:  KeepAwakeIdle,
		isBusy: isBusy,
	}
}

// keepAwakeLog writes to the archiveloop log with [keep-awake-webui] prefix.
func keepAwakeLog(format string, args ...interface{}) {
	const logPath = "/mutable/archiveloop.log"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s: [keep-awake-webui] %s\n", time.Now().Format("Mon _2 Jan 15:04:05 MST 2006"), msg)
}

// Start initiates a keep-awake session. If the system is busy, it queues the
// request in "pending" state. mode is "manual" or "auto".
func (m *KeepAwakeManager) Start(mode string, duration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already active/pending, just update expiration
	if m.state == KeepAwakeActive {
		m.expiresAt = time.Now().Add(duration)
		m.mode = mode
		return nil
	}
	if m.state == KeepAwakePending {
		m.pendingDuration = duration
		m.mode = mode
		return nil
	}

	// Stop any previous goroutine
	m.stopInternal()

	m.mode = mode
	m.pendingDuration = duration
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})

	if m.isBusy() {
		m.state = KeepAwakePending
		keepAwakeLog("Queued (mode: %s, duration: %s) — waiting for archive/processing to finish", mode, duration)
		log.Printf("[keep-awake] Queued (mode: %s) — system busy", mode)
		go m.waitForIdleThenStart()
	} else {
		m.state = KeepAwakeActive
		m.startedAt = time.Now()
		m.expiresAt = time.Now().Add(duration)
		keepAwakeLog("Started (mode: %s, duration: %s)", mode, duration)
		log.Printf("[keep-awake] Started (mode: %s, duration: %s)", mode, duration)
		go startKeepAwakeFn(keepAwakeReasonLabel(mode), m.expiresAt)
		go m.expirationWatcher()
	}

	return nil
}

// Heartbeat extends the keep-awake timer (auto mode). If idle, starts a new
// auto session. Returns the current state.
func (m *KeepAwakeManager) Heartbeat() KeepAwakeState {
	m.mu.Lock()
	defer m.mu.Unlock()

	const autoTimeout = 10 * time.Minute

	switch m.state {
	case KeepAwakeActive:
		m.expiresAt = time.Now().Add(autoTimeout)
		return KeepAwakeActive
	case KeepAwakePending:
		m.pendingDuration = autoTimeout
		return KeepAwakePending
	default:
		// Start a new auto session
		m.mu.Unlock()
		m.Start("auto", autoTimeout)
		m.mu.Lock()
		return m.state
	}
}

// Stop immediately stops/cancels keep-awake.
func (m *KeepAwakeManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	wasActive := m.state == KeepAwakeActive
	busy := m.isBusy()
	m.stopInternal()

	switch {
	case wasActive && busy:
		keepAwakeLog("Stopped by user — archive/processing still active, nudge left in archive's ownership")
		log.Printf("[keep-awake] Stopped by user (busy — nudge left alone)")
	case wasActive:
		keepAwakeLog("Stopped by user")
		log.Printf("[keep-awake] Stopped by user")
		go stopKeepAwakeFn()
	default:
		keepAwakeLog("Cancelled (was pending)")
		log.Printf("[keep-awake] Cancelled (was pending)")
	}
}

// stopInternal stops the background goroutine without releasing the lock.
func (m *KeepAwakeManager) stopInternal() {
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	m.state = KeepAwakeIdle
	m.expiresAt = time.Time{}
	m.startedAt = time.Time{}
}

// Status returns the current keep-awake status.
func (m *KeepAwakeManager) Status() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := map[string]interface{}{
		"state": string(m.state),
		"mode":  m.mode,
	}

	if m.state == KeepAwakeActive {
		remaining := time.Until(m.expiresAt)
		if remaining < 0 {
			remaining = 0
		}
		result["expires_at"] = m.expiresAt.Format(time.RFC3339)
		result["remaining_sec"] = int(remaining.Seconds())
	}

	return result
}

// waitForIdleThenStart polls until the system is no longer busy, then starts
// the keep-awake.
func (m *KeepAwakeManager) waitForIdleThenStart() {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			if !m.isBusy() {
				m.mu.Lock()
				if m.state != KeepAwakePending {
					m.mu.Unlock()
					return
				}
				m.state = KeepAwakeActive
				m.startedAt = time.Now()
				m.expiresAt = time.Now().Add(m.pendingDuration)
				mode := m.mode
				dur := m.pendingDuration
				m.mu.Unlock()

				keepAwakeLog("Started (mode: %s, duration: %s) — archive/processing finished", mode, dur)
				log.Printf("[keep-awake] Started (mode: %s) — system now idle", mode)
				go startKeepAwakeFn(keepAwakeReasonLabel(mode), m.expiresAt)
				go m.expirationWatcher()
				return
			}
		}
	}
}

// expirationWatcher monitors the expiration time and stops keep-awake when it expires.
// It also detects when archiving finishes (busy→idle) while keep-awake is still active
// and re-launches the nudge loop, because archiveloop's awake_stop kills our nudge.
func (m *KeepAwakeManager) expirationWatcher() {
	ticker := time.NewTicker(expirationTickInterval)
	defer ticker.Stop()

	wasBusy := m.isBusy()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mu.RLock()
			expired := m.state == KeepAwakeActive && time.Now().After(m.expiresAt)
			active := m.state == KeepAwakeActive
			mode := m.mode
			expiresAt := m.expiresAt
			m.mu.RUnlock()

			if expired {
				m.mu.Lock()
				if m.state == KeepAwakeActive {
					busyNow := m.isBusy()
					m.state = KeepAwakeIdle
					m.expiresAt = time.Time{}
					m.startedAt = time.Time{}
					if busyNow {
						// Archive/processing owns the nudge loop and will stop it
						// when it finishes. Killing it now would interrupt archive.
						keepAwakeLog("Expired while archive/processing active — leaving nudge alone (owned by archive)")
						log.Printf("[keep-awake] Expired while busy — leaving nudge alone")
					} else {
						keepAwakeLog("Expired, stopping keep-awake")
						log.Printf("[keep-awake] Expired")
						go stopKeepAwakeFn()
					}
				}
				if m.stopCh != nil {
					close(m.stopCh)
					m.stopCh = nil
				}
				m.mu.Unlock()
				return
			}

			// Re-arm: if archiving just finished while we're still active,
			// re-launch the nudge loop (archiveloop's awake_stop killed ours).
			nowBusy := m.isBusy()
			if wasBusy && !nowBusy && active {
				keepAwakeLog("Archive/processing finished — re-launching keep-awake (mode: %s)", mode)
				log.Printf("[keep-awake] Re-arming nudge after archive finished (mode: %s)", mode)
				go startKeepAwakeFn(keepAwakeReasonLabel(mode), expiresAt)
			}
			wasBusy = nowBusy
		}
	}
}

// RegisterKeepAwakeRoutes registers HTTP handlers for the keep-awake API.
func RegisterKeepAwakeRoutes(mux *http.ServeMux, kam *KeepAwakeManager) {
	mux.HandleFunc("POST /api/keep-awake/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode        string `json:"mode"`
			DurationMin int    `json:"duration_min"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if body.Mode == "" {
			body.Mode = "manual"
		}

		duration := time.Duration(body.DurationMin) * time.Minute
		if duration <= 0 {
			duration = 10 * time.Minute // default for auto mode
		}

		if err := kam.Start(body.Mode, duration); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, kam.Status())
	})

	mux.HandleFunc("POST /api/keep-awake/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		state := kam.Heartbeat()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"state": string(state),
		})
	})

	mux.HandleFunc("GET /api/keep-awake/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, kam.Status())
	})

	mux.HandleFunc("DELETE /api/keep-awake", func(w http.ResponseWriter, r *http.Request) {
		kam.Stop()
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	})
}
