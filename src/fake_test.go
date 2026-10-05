package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/branchkit/plugin-sdk-go"
)

// fakePlatform stands in for the platform: installed apps, launches, the
// frontmost app and windows, and collections with the user band composed over
// the plugin's records the way the platform's overrides compose them — `add`
// puts a record in the user band, `remove` suppresses an id, `restore` drops
// ALL user-band state for an id.
type fakePlatform struct {
	installed []branchkit.InstalledApp
	scanErr   error
	launchErr error
	newWindow bool // what new_app_window answers

	launched  []string
	newWins   []string
	opened    []string
	warps     []branchkit.NativeWarpCursorRequest
	frontmost string
	windows   []branchkit.WindowDetail
	scans     int

	base    map[string]map[string]json.RawMessage
	added   map[string]map[string]json.RawMessage
	removed map[string]map[string]bool
	idField map[string]string
}

func newFake(installed ...branchkit.InstalledApp) *fakePlatform {
	return &fakePlatform{
		installed: installed,
		base:      map[string]map[string]json.RawMessage{},
		added:     map[string]map[string]json.RawMessage{},
		removed:   map[string]map[string]bool{},
		idField:   map[string]string{appsCollection: "spoken", traitsCollection: "key"},
	}
}

func (f *fakePlatform) NativeInstalledApps() ([]branchkit.InstalledApp, error) {
	f.scans++
	return f.installed, f.scanErr
}

func (f *fakePlatform) NativeLaunchApp(req branchkit.NativeLaunchAppRequest) error {
	if f.launchErr != nil {
		return f.launchErr
	}
	f.launched = append(f.launched, req.BundleID)
	f.frontmost = req.BundleID
	return nil
}

func (f *fakePlatform) NativeNewAppWindow(req branchkit.NativeNewAppWindowRequest) (bool, error) {
	if f.newWindow {
		f.newWins = append(f.newWins, req.BundleID)
	}
	return f.newWindow, nil
}

func (f *fakePlatform) NativeOpenTarget(req branchkit.NativeOpenTargetRequest) error {
	f.opened = append(f.opened, req.Target)
	return nil
}

func (f *fakePlatform) NativeFrontmostApp() (*branchkit.NativeFrontmostAppResponse, error) {
	id := f.frontmost
	return &branchkit.NativeFrontmostAppResponse{App: &branchkit.RunningApp{BundleID: &id}}, nil
}

func (f *fakePlatform) NativeAppWindows(branchkit.NativeAppWindowsRequest) ([]branchkit.WindowDetail, error) {
	return f.windows, nil
}

func (f *fakePlatform) NativeCursor() (*branchkit.NativeCursorResponse, error) {
	return &branchkit.NativeCursorResponse{X: -1, Y: -1}, nil
}

func (f *fakePlatform) NativeWarpCursor(req branchkit.NativeWarpCursorRequest) error {
	f.warps = append(f.warps, req)
	return nil
}

func (f *fakePlatform) Replace(name string, entries []branchkit.CollectionPutEntry, _ branchkit.ReplaceScope, _ ...branchkit.ReplaceOption) (branchkit.ReplaceResult, error) {
	m := map[string]json.RawMessage{}
	for _, e := range entries {
		m[e.ID] = e.Payload
	}
	f.base[name] = m
	return branchkit.ReplaceResult{}, nil
}

func (f *fakePlatform) ListAll(name string) ([]branchkit.CollectionRecord, error) {
	composed := map[string]json.RawMessage{}
	for id, r := range f.base[name] {
		composed[id] = r
	}
	for id, r := range f.added[name] {
		composed[id] = r
	}
	for id := range f.removed[name] {
		delete(composed, id)
	}
	ids := make([]string, 0, len(composed))
	for id := range composed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]branchkit.CollectionRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, branchkit.CollectionRecord{ID: id, Payload: composed[id]})
	}
	return out, nil
}

func (f *fakePlatform) OverridesApply(req branchkit.OverridesApplyRequest) (*branchkit.OverridesApplyResponse, error) {
	c := req.Collection
	if f.added[c] == nil {
		f.added[c] = map[string]json.RawMessage{}
		f.removed[c] = map[string]bool{}
	}
	switch req.Action {
	case "add":
		var fields map[string]any
		if err := json.Unmarshal(req.Fields, &fields); err != nil {
			return nil, err
		}
		id, _ := fields[f.idField[c]].(string)
		if id == "" {
			return nil, errors.New("record has no id field")
		}
		f.added[c][id] = req.Fields
	case "remove":
		f.removed[c][*req.ID] = true
	case "restore":
		delete(f.added[c], *req.ID)
		delete(f.removed[c], *req.ID)
	}
	return &branchkit.OverridesApplyResponse{}, nil
}

func (f *fakePlatform) userBandSize() int {
	n := 0
	for _, c := range f.added {
		n += len(c)
	}
	for _, c := range f.removed {
		n += len(c)
	}
	return n
}

// testCore is a small curated file: two apps with names on macOS and Linux,
// and one trait-only terminal.
const testCore = `[
  {"name": "Google Chrome", "ids": {"macos": ["com.google.Chrome"], "linux": ["google-chrome"]}, "aliases": ["chrome", "Google Chrome"]},
  {"name": "Terminal", "ids": {"macos": ["com.apple.Terminal"], "linux": ["gnome-terminal"]}, "aliases": ["terminal"], "traits": ["terminal"]},
  {"name": "Ghostty", "ids": {"macos": ["com.mitchellh.ghostty"]}, "traits": ["terminal"], "note": "trait-only"}
]`

var (
	chrome   = branchkit.InstalledApp{Name: "Google Chrome", BundleID: "com.google.Chrome"}
	terminal = branchkit.InstalledApp{Name: "Terminal", BundleID: "com.apple.Terminal"}
	notes    = branchkit.InstalledApp{Name: "Notes", BundleID: "com.apple.Notes"}
)

// newTestHost returns a host on macOS over a fake, with the test curated file,
// after one successful scan.
func newTestHost(t *testing.T, installed ...branchkit.InstalledApp) (*Host, *fakePlatform) {
	t.Helper()
	f := newFake(installed...)
	core := filepath.Join(t.TempDir(), "core_apps.json")
	if err := os.WriteFile(core, []byte(testCore), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHost(f, core, "darwin")
	if err := h.scanAndPublish(); err != nil {
		t.Fatal(err)
	}
	return h, f
}

func mustNames(t *testing.T, h *Host) map[string]string {
	t.Helper()
	names, err := h.loadNames()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func mustRender(t *testing.T, c branchkit.HTMLComponent) string {
	t.Helper()
	html, err := branchkit.RenderComponent(c)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}
