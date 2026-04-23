package api

import (
	"sync/atomic"
	"testing"
	"time"
)

type hookCounters struct {
	start int32
	stop  int32
}

func installHooks(t *testing.T) *hookCounters {
	t.Helper()
	var h hookCounters

	origStart := startKeepAwakeFn
	origStop := stopKeepAwakeFn
	origTick := expirationTickInterval
	origIdle := idleCheckInterval

	startKeepAwakeFn = func(_ string, _ time.Time) {
		atomic.AddInt32(&h.start, 1)
	}
	stopKeepAwakeFn = func() {
		atomic.AddInt32(&h.stop, 1)
	}
	expirationTickInterval = 5 * time.Millisecond
	idleCheckInterval = 5 * time.Millisecond

	t.Cleanup(func() {
		startKeepAwakeFn = origStart
		stopKeepAwakeFn = origStop
		expirationTickInterval = origTick
		idleCheckInterval = origIdle
	})
	return &h
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func TestStart_WhileBusy_Queues(t *testing.T) {
	h := installHooks(t)
	m := NewKeepAwakeManager(func() bool { return true })

	if err := m.Start("manual", 100*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := m.Status()["state"]; got != string(KeepAwakePending) {
		t.Fatalf("expected pending, got %v", got)
	}
	if atomic.LoadInt32(&h.start) != 0 {
		t.Fatalf("startKeepAwakeFn should not be called while busy; got %d", h.start)
	}
	if atomic.LoadInt32(&h.stop) != 0 {
		t.Fatalf("stopKeepAwakeFn should not be called; got %d", h.stop)
	}
	m.Stop()
}

func TestExpiration_Idle_CallsStop(t *testing.T) {
	h := installHooks(t)
	m := NewKeepAwakeManager(func() bool { return false })

	if err := m.Start("manual", 30*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !waitFor(t, 500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&h.stop) > 0
	}) {
		t.Fatalf("expected stopKeepAwakeFn to be called after idle expiration; got %d", h.stop)
	}
	if got := m.Status()["state"]; got != string(KeepAwakeIdle) {
		t.Fatalf("expected idle after expiration, got %v", got)
	}
}

func TestExpiration_Busy_DoesNotCallStop(t *testing.T) {
	h := installHooks(t)
	var busy int32
	m := NewKeepAwakeManager(func() bool { return atomic.LoadInt32(&busy) == 1 })

	if err := m.Start("manual", 30*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := m.Status()["state"]; got != string(KeepAwakeActive) {
		t.Fatalf("expected active, got %v", got)
	}
	atomic.StoreInt32(&busy, 1)

	if !waitFor(t, 500*time.Millisecond, func() bool {
		return m.Status()["state"] == string(KeepAwakeIdle)
	}) {
		t.Fatalf("expected state to become idle after expiration while busy; still %v", m.Status()["state"])
	}
	if atomic.LoadInt32(&h.stop) != 0 {
		t.Fatalf("stopKeepAwakeFn must not be called when busy; got %d calls", h.stop)
	}
}

func TestUserStop_Busy_DoesNotCallStop(t *testing.T) {
	h := installHooks(t)
	var busy int32
	m := NewKeepAwakeManager(func() bool { return atomic.LoadInt32(&busy) == 1 })

	if err := m.Start("manual", 10*time.Minute); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := m.Status()["state"]; got != string(KeepAwakeActive) {
		t.Fatalf("expected active, got %v", got)
	}
	atomic.StoreInt32(&busy, 1)

	m.Stop()

	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&h.stop) != 0 {
		t.Fatalf("user Stop while busy must not call stopKeepAwakeFn; got %d calls", h.stop)
	}
	if got := m.Status()["state"]; got != string(KeepAwakeIdle) {
		t.Fatalf("expected idle after Stop, got %v", got)
	}
}

func TestUserStop_Idle_CallsStop(t *testing.T) {
	h := installHooks(t)
	m := NewKeepAwakeManager(func() bool { return false })

	if err := m.Start("manual", 10*time.Minute); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop()

	if !waitFor(t, 200*time.Millisecond, func() bool {
		return atomic.LoadInt32(&h.stop) > 0
	}) {
		t.Fatal("user Stop while idle must call stopKeepAwakeFn")
	}
}
