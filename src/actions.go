package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// Action handlers. Each returns its error: a failed action that reports
// success is invisible to whoever ran it — the person who spoke, a script's
// dispatch() result, a keybind — and to every report built on dispatch.
//
// Param structs (LaunchParams, …) are generated into actions_gen.go from
// plugin.json's action_types; edit the manifest and run branchkit-gen.

// resolveApp turns what an action was given into an app id. Voice hands over
// an id already (the value a spoken name resolves to); a keybind or a script
// may give a name, so a name the collection knows resolves too. Anything else
// is passed through as an id for the platform to accept or refuse.
func (h *Host) resolveApp(want string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return "", fmt.Errorf("no app given")
	}
	for _, a := range h.getApps() {
		if a.ID == want {
			return want, nil
		}
	}
	if names, err := h.loadNames(); err == nil {
		if id, ok := names[spokenForm(want)]; ok {
			return id, nil
		}
	}
	return want, nil
}

func (h *Host) handleLaunch(p LaunchParams, _ *branchkit.OnActionRequest) (any, error) {
	id, err := h.resolveApp(p.AppID)
	if err != nil {
		return nil, err
	}
	newInstance := p.NewInstance != nil && *p.NewInstance
	if err := h.plugin.NativeLaunchApp(branchkit.NativeLaunchAppRequest{BundleID: id, NewInstance: &newInstance}); err != nil {
		return nil, fmt.Errorf("open %s: %w", h.displayName(id), err)
	}
	if h.LoadConfig().PointerToOpenedApp {
		go h.movePointerToApp(id, time.Sleep)
	}
	return nil, nil
}

// handleNewWindow opens a NEW window of an app on the CURRENT desktop,
// without switching to the desktop the app's other windows are on (which a
// plain launch would do by raising its frontmost window). The platform's
// new_app_window reports whether the app could make one that way; when it
// cannot, this falls back to a plain launch.
func (h *Host) handleNewWindow(p NewWindowParams, _ *branchkit.OnActionRequest) (any, error) {
	id, err := h.resolveApp(p.AppID)
	if err != nil {
		return nil, err
	}
	made, err := h.plugin.NativeNewAppWindow(branchkit.NativeNewAppWindowRequest{BundleID: id})
	if err == nil && made {
		return nil, nil
	}
	branchkit.Logf("apps", "new_window: %s could not make a window here (err=%v); opening it instead", id, err)
	noNewInstance := false
	if err := h.plugin.NativeLaunchApp(branchkit.NativeLaunchAppRequest{BundleID: id, NewInstance: &noNewInstance}); err != nil {
		return nil, fmt.Errorf("open %s: %w", h.displayName(id), err)
	}
	return nil, nil
}

func (h *Host) handleOpen(p OpenParams, _ *branchkit.OnActionRequest) (any, error) {
	if strings.TrimSpace(p.Target) == "" {
		return nil, fmt.Errorf("nothing to open")
	}
	if err := h.plugin.NativeOpenTarget(branchkit.NativeOpenTargetRequest{Target: p.Target}); err != nil {
		return nil, fmt.Errorf("open %s: %w", p.Target, err)
	}
	return nil, nil
}

// movePointerToApp moves the pointer to the centre of the app's focused (or
// first visible) window, so scrolling lands in the app just opened.
//
// launch_app returns before the OS finishes activating the app: the desktop
// switch and window raise settle afterwards, and windows on another desktop
// report placeholder bounds. Moving from that state lands on the wrong window
// or display, so wait until the app is actually frontmost, then ask for its
// now-visible window. An app that never comes forward in time, or has no
// window yet, is left alone.
func (h *Host) movePointerToApp(appID string, sleep func(time.Duration)) {
	for waited := time.Duration(0); ; waited += 50 * time.Millisecond {
		if front, err := h.plugin.NativeFrontmostApp(); err == nil &&
			front.App != nil && front.App.BundleID != nil && strings.EqualFold(*front.App.BundleID, appID) {
			break
		}
		if waited >= 1500*time.Millisecond {
			return
		}
		sleep(50 * time.Millisecond)
	}
	// One more beat for the window raise after the app comes forward.
	sleep(80 * time.Millisecond)

	wins, err := h.plugin.NativeAppWindows(branchkit.NativeAppWindowsRequest{BundleID: appID})
	if err != nil {
		branchkit.Logf("apps", "pointer: windows of %s: %v", appID, err)
		return
	}
	var cursor *branchkit.NativeCursorResponse
	if cur, err := h.plugin.NativeCursor(); err == nil {
		cursor = cur
	}
	target := pointerTarget(wins, cursor)
	if target == nil {
		return
	}
	if err := h.plugin.NativeWarpCursor(branchkit.NativeWarpCursorRequest{X: target.X, Y: target.Y}); err != nil {
		branchkit.Logf("apps", "pointer: move: %v", err)
	}
}

type point struct{ X, Y int }

// pointerTarget returns the centre of the focused window (else the first
// window that is not minimized), or nil when there is none or the pointer is
// already inside it.
func pointerTarget(wins []branchkit.WindowDetail, cursor *branchkit.NativeCursorResponse) *point {
	var target *branchkit.WindowDetail
	for i := range wins {
		w := &wins[i]
		if w.IsMinimized {
			continue
		}
		if w.IsFocused {
			target = w
			break
		}
		if target == nil {
			target = w
		}
	}
	if target == nil {
		return nil
	}
	b := target.Bounds
	if cursor != nil && cursor.X >= b.X && cursor.X < b.X+b.W && cursor.Y >= b.Y && cursor.Y < b.Y+b.H {
		return nil
	}
	return &point{X: b.X + b.W/2, Y: b.Y + b.H/2}
}
