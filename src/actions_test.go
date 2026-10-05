package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

func TestLaunch_ByIDAndByName(t *testing.T) {
	h, f := newTestHost(t, chrome)
	if _, err := h.handleLaunch(LaunchParams{AppID: chrome.BundleID}, nil); err != nil {
		t.Fatal(err)
	}
	// A keybind or script may give a name the collection knows.
	if _, err := h.handleLaunch(LaunchParams{AppID: "Chrome"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.launched) != 2 || f.launched[0] != chrome.BundleID || f.launched[1] != chrome.BundleID {
		t.Errorf("launched = %v", f.launched)
	}
}

// A failed launch is reported, not swallowed.
func TestLaunch_ReturnsErrors(t *testing.T) {
	h, f := newTestHost(t, chrome)
	if _, err := h.handleLaunch(LaunchParams{}, nil); err == nil {
		t.Error("an empty app was accepted")
	}
	f.launchErr = errors.New("no such app")
	_, err := h.handleLaunch(LaunchParams{AppID: chrome.BundleID}, nil)
	if err == nil || !strings.Contains(err.Error(), "Google Chrome") {
		t.Errorf("err = %v, want one naming the app", err)
	}
	if _, err := h.handleOpen(OpenParams{Target: "  "}, nil); err == nil {
		t.Error("an empty target was accepted")
	}
}

// new_window uses the platform's same-desktop window when the app can make
// one, and falls back to opening the app when it cannot.
func TestNewWindow(t *testing.T) {
	h, f := newTestHost(t, chrome)
	f.newWindow = true
	if _, err := h.handleNewWindow(NewWindowParams{AppID: chrome.BundleID}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.newWins) != 1 || len(f.launched) != 0 {
		t.Fatalf("scriptable: newWins=%v launched=%v", f.newWins, f.launched)
	}
	f.newWindow = false
	if _, err := h.handleNewWindow(NewWindowParams{AppID: chrome.BundleID}, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.launched) != 1 {
		t.Errorf("fallback did not launch: %v", f.launched)
	}
}

func TestMovePointerToApp(t *testing.T) {
	h, f := newTestHost(t, chrome)
	f.frontmost = chrome.BundleID
	f.windows = []branchkit.WindowDetail{{IsFocused: true, Bounds: branchkit.WindowBounds{X: 100, Y: 100, W: 200, H: 100}}}
	h.movePointerToApp(chrome.BundleID, func(time.Duration) {})
	if len(f.warps) != 1 || f.warps[0].X != 200 || f.warps[0].Y != 150 {
		t.Errorf("warps = %+v", f.warps)
	}
	// An app that never comes forward is left alone.
	f.frontmost, f.warps = "com.example.other", nil
	h.movePointerToApp(chrome.BundleID, func(time.Duration) {})
	if len(f.warps) != 0 {
		t.Errorf("moved to an app that never came forward: %+v", f.warps)
	}
}

func TestPointerTarget(t *testing.T) {
	minimized := branchkit.WindowDetail{IsMinimized: true, Bounds: branchkit.WindowBounds{X: 0, Y: 0, W: 10, H: 10}}
	first := branchkit.WindowDetail{Bounds: branchkit.WindowBounds{X: 0, Y: 0, W: 100, H: 100}}
	focused := branchkit.WindowDetail{IsFocused: true, Bounds: branchkit.WindowBounds{X: 200, Y: 0, W: 100, H: 100}}
	if p := pointerTarget([]branchkit.WindowDetail{minimized, first, focused}, nil); p == nil || p.X != 250 {
		t.Errorf("want the focused window's centre, got %+v", p)
	}
	if p := pointerTarget([]branchkit.WindowDetail{minimized, first}, nil); p == nil || p.X != 50 {
		t.Errorf("want the first visible window's centre, got %+v", p)
	}
	if p := pointerTarget([]branchkit.WindowDetail{minimized}, nil); p != nil {
		t.Errorf("want nil with only minimized windows, got %+v", p)
	}
	inside := &branchkit.NativeCursorResponse{X: 250, Y: 50}
	if p := pointerTarget([]branchkit.WindowDetail{focused}, inside); p != nil {
		t.Errorf("want nil when the pointer is already inside, got %+v", p)
	}
}
