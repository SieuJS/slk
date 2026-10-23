// internal/ui/app_presence_test.go
//
// Phase 0 characterization tests for the presence/DND state mutations
// on App: applyOptimisticStatus (called by the presence menu and
// custom-snooze flow) and the StatusChangeMsg handler in Update
// (called by the WS event router). These pin behavior for the future
// presenceController extraction.
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/presencemenu"
)

func TestApplyOptimisticStatusSetActive(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// Seed: was previously away with DND on, just to be sure we don't
	// stomp DND fields with this action.
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(5 * time.Minute),
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetActive, 0)

	got := a.presence.byTeam["T1"]
	if got.Presence != "active" {
		t.Errorf("Presence: want %q, got %q", "active", got.Presence)
	}
	if !got.DNDEnabled {
		t.Error("ActionSetActive must not clear DNDEnabled")
	}
	if got.DNDEndTS.IsZero() {
		t.Error("ActionSetActive must not clear DNDEndTS")
	}
}

func TestApplyOptimisticStatusSetAway(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{Presence: "active"}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetAway, 0)

	if got := a.presence.byTeam["T1"].Presence; got != "away" {
		t.Errorf("Presence: want %q, got %q", "away", got)
	}
}

func TestApplyOptimisticStatusSnoozeSetsDND(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{Presence: "active"}

	before := time.Now()
	a.presence.Apply(a.activeTeamID, presencemenu.ActionSnooze, 30)
	after := time.Now()

	got := a.presence.byTeam["T1"]
	if !got.DNDEnabled {
		t.Fatal("expected DNDEnabled=true after Snooze")
	}
	// DNDEndTS must be ~30m from now (loose bounds to handle test jitter).
	earliest := before.Add(30 * time.Minute)
	latest := after.Add(30 * time.Minute)
	if got.DNDEndTS.Before(earliest) || got.DNDEndTS.After(latest) {
		t.Errorf("DNDEndTS=%v out of expected window [%v..%v]",
			got.DNDEndTS, earliest, latest)
	}
	// Presence is unchanged by Snooze.
	if got.Presence != "active" {
		t.Errorf("Snooze must not change Presence; got %q", got.Presence)
	}
}

func TestApplyOptimisticStatusEndDNDClearsDND(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:   "active",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(10 * time.Minute),
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionEndDND, 0)

	got := a.presence.byTeam["T1"]
	if got.DNDEnabled {
		t.Error("DNDEnabled must be false after ActionEndDND")
	}
	if !got.DNDEndTS.IsZero() {
		t.Errorf("DNDEndTS must be zero after ActionEndDND; got %v", got.DNDEndTS)
	}
	// Presence is unchanged by EndDND.
	if got.Presence != "active" {
		t.Errorf("EndDND must not change Presence; got %q", got.Presence)
	}
}

func TestApplyOptimisticStatusInitializesMissingTeamEntry(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// statusByTeam["T1"] does not exist yet.

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetAway, 0)

	got, ok := a.presence.byTeam["T1"]
	if !ok {
		t.Fatal("expected entry for T1 to be created on first apply")
	}
	if got.Presence != "away" {
		t.Errorf("Presence: want %q, got %q", "away", got.Presence)
	}
}

func TestApplyOptimisticStatusSetWFHSetsStatusText(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{Presence: "active"}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetWFH, 0)

	text, emoji := a.presence.CustomStatus("T1")
	if text != "Working from home" {
		t.Errorf("StatusText: want %q, got %q", "Working from home", text)
	}
	if emoji == "" {
		t.Error("expected a non-empty display emoji for WFH")
	}
	// Presence is unchanged by setting a custom status.
	if got := a.presence.byTeam["T1"].Presence; got != "active" {
		t.Errorf("ActionSetWFH must not change Presence; got %q", got)
	}
}

func TestApplyOptimisticStatusSetVacationSetsStatusText(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetVacation, 0)

	text, _ := a.presence.CustomStatus("T1")
	if text != "On vacation" {
		t.Errorf("StatusText: want %q, got %q", "On vacation", text)
	}
}

func TestApplyOptimisticStatusClearStatusClearsText(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:    "active",
		StatusText:  "On vacation",
		StatusEmoji: "🌴",
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionClearStatus, 0)

	text, emoji := a.presence.CustomStatus("T1")
	if text != "" || emoji != "" {
		t.Errorf("expected cleared status, got text=%q emoji=%q", text, emoji)
	}
	// Presence is unchanged by clearing the custom status.
	if got := a.presence.byTeam["T1"].Presence; got != "active" {
		t.Errorf("ActionClearStatus must not change Presence; got %q", got)
	}
}

// TestApplyOptimisticStatusSetActiveDoesNotClearCustomStatus pins the
// invariant that presence and custom status are independent in real
// Slack: picking Active/Away must not wipe an existing custom status.
// An earlier draft of this feature got this backwards.
func TestApplyOptimisticStatusSetActiveDoesNotClearCustomStatus(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:    "away",
		StatusText:  "Working from home",
		StatusEmoji: "🏠",
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetActive, 0)

	text, emoji := a.presence.CustomStatus("T1")
	if text != "Working from home" || emoji != "🏠" {
		t.Errorf("ActionSetActive must not clear custom status, got text=%q emoji=%q", text, emoji)
	}
}

func TestApplyOptimisticStatusSetAwayDoesNotClearCustomStatus(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.byTeam["T1"] = workspaceStatus{
		Presence:    "active",
		StatusText:  "On vacation",
		StatusEmoji: "🌴",
	}

	a.presence.Apply(a.activeTeamID, presencemenu.ActionSetAway, 0)

	text, _ := a.presence.CustomStatus("T1")
	if text != "On vacation" {
		t.Errorf("ActionSetAway must not clear custom status, got text=%q", text)
	}
}

func TestCustomStatusEmptyForUnknownTeam(t *testing.T) {
	a := NewApp()

	text, emoji := a.presence.CustomStatus("no-such-team")
	if text != "" || emoji != "" {
		t.Errorf("expected empty status for unknown team, got text=%q emoji=%q", text, emoji)
	}
}

// TestStatusChangeMsgPreservesCustomStatus pins a real bug found in
// review: StatusChangeMsg arrives on every reconnect and on any
// presence/DND change event from Slack, not just ones the user
// triggered. Set() used to rebuild workspaceStatus from scratch,
// silently wiping an existing custom status on the very next
// reconnect or presence event — even though Slack still has it set
// server-side.
func TestStatusChangeMsgPreservesCustomStatus(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	a.presence.Apply("T1", presencemenu.ActionSetWFH, 0)
	// Mirror what the real Ctrl+S selection does (applyPresenceAction):
	// push the just-set status to the status bar, so this test can
	// prove the DISPLAY survives the event, not only the cache.
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("T1"))

	_, _ = a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: false,
	})

	text, emoji := a.presence.CustomStatus("T1")
	if text != "Working from home" || emoji != "🏠" {
		t.Errorf("StatusChangeMsg must preserve custom status, got text=%q emoji=%q", text, emoji)
	}
	if !strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar must still display the custom status after StatusChangeMsg")
	}
	// Presence itself must still update normally.
	if got := a.presence.byTeam["T1"].Presence; got != "away" {
		t.Errorf("Presence: want %q, got %q", "away", got)
	}
}

// TestWorkspaceSwitchRefreshesCustomStatusSegment pins a real bug
// found in review: switching workspaces refreshed the statusbar's
// presence/DND segment but never its custom-status segment, so a
// status set in one workspace kept showing after switching to a
// workspace with no status of its own.
func TestWorkspaceSwitchRefreshesCustomStatusSegment(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.presence.Apply("T1", presencemenu.ActionSetWFH, 0)
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("T1"))

	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta"})

	if strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar still shows T1's custom status after switching to T2")
	}
}

// TestWorkspaceSwitchRoundTripRestoresCustomStatus extends the above:
// each workspace's custom status is cached independently (keyed by
// team ID), so switching away and back must restore it rather than
// permanently losing it the first time the status bar is cleared.
func TestWorkspaceSwitchRoundTripRestoresCustomStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.presence.Apply("T1", presencemenu.ActionSetWFH, 0)
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("T1"))

	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta"})
	a.presence.Apply("T2", presencemenu.ActionSetVacation, 0)
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("T2"))

	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T1", TeamName: "acme"})

	out := a.statusbar.View(120)
	if !strings.Contains(out, "Working from home") {
		t.Errorf("expected T1's custom status restored on switch-back, got: %q", out)
	}
	if strings.Contains(out, "On vacation") {
		t.Errorf("T2's custom status leaked into T1's status bar: %q", out)
	}
}

// TestInitialActiveActivationRefreshesCustomStatusSegment pins a race
// found in review: workspaces connect concurrently, and router.Add
// happens for every one of them regardless of InitialActive — so a
// non-default workspace can finish connecting, be switched to, and
// have a status set on it before the (slower-connecting) configured
// default workspace's WorkspaceReadyMsg arrives and claims
// InitialActive. That claim must still refresh the custom-status
// segment to the newly-active workspace's own cache, not leave the
// previous workspace's status stuck on screen.
func TestInitialActiveActivationRefreshesCustomStatusSegment(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T-other"))
	a.presence.Apply("T-other", presencemenu.ActionSetWFH, 0)
	a.statusbar.SetCustomStatus(a.presence.CustomStatus("T-other"))

	_, _ = a.Update(WorkspaceReadyMsg{
		TeamID:        "T-default",
		InitialActive: true,
	})

	if strings.Contains(a.statusbar.View(120), "Working from home") {
		t.Error("status bar still shows the previous workspace's custom status after InitialActive activation")
	}
}

func TestStatusChangeMsgUpdatesPerTeamCache(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	dndEnd := time.Now().Add(1 * time.Hour)

	// Status arrives for a DIFFERENT workspace: must update the cache
	// but must NOT touch the active workspace's status bar.
	_, _ = a.Update(StatusChangeMsg{
		TeamID:     "T2",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})

	got, ok := a.presence.byTeam["T2"]
	if !ok {
		t.Fatal("expected statusByTeam[T2] to be set")
	}
	if got.Presence != "away" || !got.DNDEnabled || !got.DNDEndTS.Equal(dndEnd) {
		t.Errorf("T2 cache mismatch: got %+v", got)
	}
	// T1 (active) must not have been written.
	if _, exists := a.presence.byTeam["T1"]; exists {
		t.Error("active T1 entry should not be created by a T2 status change")
	}
}

func TestStatusChangeMsgForActiveTeamStartsDNDTickerOnce(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	dndEnd := time.Now().Add(1 * time.Hour)

	// First StatusChangeMsg for the active team with DND enabled in the
	// future flips dndTickerOn and schedules a tick.
	_, cmd := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})
	if !a.presence.dndTickerOn {
		t.Fatal("expected dndTickerOn=true after first DND-active StatusChangeMsg")
	}
	if cmd == nil {
		t.Fatal("expected a tea.Cmd (the DNDTick scheduler) to be returned")
	}

	// A second identical StatusChangeMsg must NOT re-schedule (the
	// dndTickerOn guard prevents parallel tick chains).
	_, cmd2 := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   dndEnd,
	})
	if cmd2 != nil {
		t.Error("second DND-active StatusChangeMsg must not schedule another tick")
	}
}

func TestStatusChangeMsgExpiredDNDDoesNotStartTicker(t *testing.T) {
	a := NewApp()
	a.activeTeamID = "T1"
	// DNDEndTS already in the past → no tick should be scheduled even
	// though DNDEnabled is true (the renderer falls back to presence).
	_, cmd := a.Update(StatusChangeMsg{
		TeamID:     "T1",
		Presence:   "away",
		DNDEnabled: true,
		DNDEndTS:   time.Now().Add(-1 * time.Minute),
	})
	if a.presence.dndTickerOn {
		t.Error("ticker must not start for already-expired DND")
	}
	if cmd != nil {
		t.Error("expired DND must not return a tick cmd")
	}
}
