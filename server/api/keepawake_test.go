package api

import (
	"os"
	"path/filepath"
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
	origFlag := keepAwakeWantedFlagPath

	startKeepAwakeFn = func(_ string, _ time.Time) {
		atomic.AddInt32(&h.start, 1)
	}
	stopKeepAwakeFn = func() {
		atomic.AddInt32(&h.stop, 1)
	}
	expirationTickInterval = 5 * time.Millisecond
	idleCheckInterval = 5 * time.Millisecond
	keepAwakeWantedFlagPath = filepath.Join(t.TempDir(), "keep_awake_wanted")
	resetKeepAwakeRegistry()

	t.Cleanup(func() {
		startKeepAwakeFn = origStart
		stopKeepAwakeFn = origStop
		expirationTickInterval = origTick
		idleCheckInterval = origIdle
		keepAwakeWantedFlagPath = origFlag
		resetKeepAwakeRegistry()
	})
	return &h
}

func wantedFlagExists() bool {
	_, err := os.Stat(keepAwakeWantedFlagPath)
	return err == nil
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

func TestWantedFlag_LifecycleActive(t *testing.T) {
	installHooks(t)
	m := NewKeepAwakeManager(func() bool { return false })

	if wantedFlagExists() {
		t.Fatal("flag should not exist before Start")
	}
	if err := m.Start("manual", 10*time.Minute); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !wantedFlagExists() {
		t.Fatal("flag must exist while Active")
	}
	m.Stop()
	if wantedFlagExists() {
		t.Fatal("flag must be cleared after Stop")
	}
}

func TestWantedFlag_LifecyclePending(t *testing.T) {
	installHooks(t)
	m := NewKeepAwakeManager(func() bool { return true })

	if err := m.Start("manual", 10*time.Minute); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !wantedFlagExists() {
		t.Fatal("flag must exist while Pending")
	}
	m.Stop()
	if wantedFlagExists() {
		t.Fatal("flag must be cleared after Stop from Pending")
	}
}

func TestWantedFlag_ClearedOnExpire(t *testing.T) {
	installHooks(t)
	m := NewKeepAwakeManager(func() bool { return false })

	if err := m.Start("manual", 30*time.Millisecond); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !wantedFlagExists() {
		t.Fatal("flag must exist immediately after Active start")
	}
	if !waitFor(t, 500*time.Millisecond, func() bool {
		return m.Status()["state"] == string(KeepAwakeIdle)
	}) {
		t.Fatal("expected idle after expiration")
	}
	if wantedFlagExists() {
		t.Fatal("flag must be cleared after expire → idle")
	}
}

func TestWantedFlag_StaleClearedByConstructor(t *testing.T) {
	origFlag := keepAwakeWantedFlagPath
	keepAwakeWantedFlagPath = filepath.Join(t.TempDir(), "stale")
	t.Cleanup(func() {
		keepAwakeWantedFlagPath = origFlag
		resetKeepAwakeRegistry()
	})

	if err := os.WriteFile(keepAwakeWantedFlagPath, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = NewKeepAwakeManager(func() bool { return false })
	if wantedFlagExists() {
		t.Fatal("constructor must clear a stale flag left from a prior run")
	}
}

func TestRegistry_MultipleOwnersHoldFlag(t *testing.T) {
	installHooks(t)

	registerKeepAwakeWant("processor")
	if !wantedFlagExists() {
		t.Fatal("flag must exist after first register")
	}
	registerKeepAwakeWant("migration")
	if !wantedFlagExists() {
		t.Fatal("flag must still exist with two owners")
	}

	if last := releaseKeepAwakeWant("processor"); last {
		t.Fatal("release with other owner still registered must return wasLast=false")
	}
	if !wantedFlagExists() {
		t.Fatal("flag must still exist while migration holds")
	}

	if last := releaseKeepAwakeWant("migration"); !last {
		t.Fatal("last release must return wasLast=true")
	}
	if wantedFlagExists() {
		t.Fatal("flag must be cleared after last release")
	}
}

func TestRegistry_RegisterIsIdempotent(t *testing.T) {
	installHooks(t)

	registerKeepAwakeWant("processor")
	registerKeepAwakeWant("processor") // duplicate — must not corrupt count
	if last := releaseKeepAwakeWant("processor"); !last {
		t.Fatal("single logical owner should release in one call even after duplicate register")
	}
	if wantedFlagExists() {
		t.Fatal("flag must be gone after the sole owner releases")
	}
}

func TestRegistry_WebuiAndProcessorCoexist(t *testing.T) {
	installHooks(t)
	m := NewKeepAwakeManager(func() bool { return false })

	if err := m.Start("manual", 10*time.Minute); err != nil {
		t.Fatalf("Start: %v", err)
	}
	registerKeepAwakeWant("processor")
	if !wantedFlagExists() {
		t.Fatal("flag must exist with webui + processor")
	}

	// Webui stops — processor still holds.
	m.Stop()
	if !wantedFlagExists() {
		t.Fatal("flag must remain while processor still holds")
	}

	// Processor releases — flag clears.
	if last := releaseKeepAwakeWant("processor"); !last {
		t.Fatal("processor release should be last")
	}
	if wantedFlagExists() {
		t.Fatal("flag must be gone after processor release")
	}
}
