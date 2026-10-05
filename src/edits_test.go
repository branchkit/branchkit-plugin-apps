package main

import (
	"strings"
	"testing"
)

// The Apps tab shows the user's edits as they are saved, not this plugin's
// memory — after a restart as much as before. A restart is a new host over
// the same saved collections.
func TestTabReflectsSavedEditsAfterRestart(t *testing.T) {
	h, f := newTestHost(t, chrome, notes)
	if err := h.addAppName(chrome.BundleID, "browser"); err != nil {
		t.Fatal(err)
	}
	if err := h.removeAppName(notes.BundleID, "notes"); err != nil {
		t.Fatal(err)
	}

	restarted := newHost(f, h.coreFile, "darwin")
	if err := restarted.scanAndPublish(); err != nil {
		t.Fatal(err)
	}
	html, err := restarted.renderAppsTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `Stop &#34;browser&#34; opening Google Chrome`) {
		t.Error("a name the user added is missing after a restart")
	}
	if strings.Contains(html, `Stop &#34;notes&#34; opening Notes`) {
		t.Error("a name the user removed is back after a restart")
	}
	// Notes has no names left, so it shows as off.
	rows := appRows(restarted.getApps(), mustNames(t, restarted), traitCatalog{}, "")
	for _, r := range rows {
		if r.ID == notes.BundleID && r.Enabled {
			t.Error("an app with every name removed shows as on")
		}
	}
}

func TestAddName_RefusesAnotherAppsName(t *testing.T) {
	h, f := newTestHost(t, chrome, terminal)
	err := h.addAppName(chrome.BundleID, "Terminal")
	if err == nil || !strings.Contains(err.Error(), "already names Terminal") {
		t.Fatalf("err = %v", err)
	}
	if f.userBandSize() != 0 {
		t.Error("a refused name left user-band state")
	}
}

func TestRemoveUserName_LeavesNothing(t *testing.T) {
	h, f := newTestHost(t, chrome)
	if err := h.addAppName(chrome.BundleID, "browser"); err != nil {
		t.Fatal(err)
	}
	if err := h.removeAppName(chrome.BundleID, "browser"); err != nil {
		t.Fatal(err)
	}
	if _, ok := mustNames(t, h)["browser"]; ok {
		t.Error("removed name still there")
	}
	if f.userBandSize() != 0 {
		t.Errorf("user band holds %d entries after add+remove, want 0", f.userBandSize())
	}
}

func TestRemoveThenAddShippedName_RoundTripsToNothing(t *testing.T) {
	h, f := newTestHost(t, chrome)
	if err := h.removeAppName(chrome.BundleID, "chrome"); err != nil {
		t.Fatal(err)
	}
	if _, ok := mustNames(t, h)["chrome"]; ok {
		t.Fatal("shipped name still there after removal")
	}
	if err := h.addAppName(chrome.BundleID, "chrome"); err != nil {
		t.Fatal(err)
	}
	if mustNames(t, h)["chrome"] != chrome.BundleID {
		t.Fatal("shipped name not back")
	}
	if f.userBandSize() != 0 {
		t.Errorf("user band holds %d entries, want 0", f.userBandSize())
	}
}

// Off removes every name; on puts back the ones this plugin publishes. The
// state is read from the collection, so it survives a restart.
func TestSetAppEnabled(t *testing.T) {
	h, f := newTestHost(t, chrome)
	if err := h.setAppEnabled(chrome.BundleID, false); err != nil {
		t.Fatal(err)
	}
	for spoken, id := range mustNames(t, h) {
		if id == chrome.BundleID {
			t.Errorf("%q still opens chrome after turning it off", spoken)
		}
	}
	if err := h.setAppEnabled(chrome.BundleID, true); err != nil {
		t.Fatal(err)
	}
	names := mustNames(t, h)
	if names["chrome"] != chrome.BundleID || names["google chrome"] != chrome.BundleID {
		t.Errorf("names after turning back on: %v", names)
	}
	if f.userBandSize() != 0 {
		t.Errorf("user band holds %d entries after off+on, want 0", f.userBandSize())
	}
}

func TestTraitEdits_RoundTripToNothing(t *testing.T) {
	h, f := newTestHost(t, chrome, terminal)
	if err := h.removeTrait(terminal.BundleID, "terminal"); err != nil {
		t.Fatal(err)
	}
	if h.hasTrait(terminal.BundleID, "terminal") {
		t.Fatal("shipped trait still there after removal")
	}
	if err := h.addTrait(terminal.BundleID, "Terminal"); err != nil {
		t.Fatal(err)
	}
	if err := h.addTrait(chrome.BundleID, "chat"); err != nil {
		t.Fatal(err)
	}
	if err := h.removeTrait(chrome.BundleID, "chat"); err != nil {
		t.Fatal(err)
	}
	if !h.hasTrait(terminal.BundleID, "terminal") || h.hasTrait(chrome.BundleID, "chat") {
		t.Fatal("traits not back to what ships")
	}
	if f.userBandSize() != 0 {
		t.Errorf("user band holds %d entries, want 0", f.userBandSize())
	}
}

func TestNormalizeTrait(t *testing.T) {
	for raw, want := range map[string]string{" Terminal ": "terminal", "big_screen": "big_screen", "a-b": "a-b"} {
		if got, err := normalizeTrait(raw); err != nil || got != want {
			t.Errorf("normalizeTrait(%q) = %q, %v", raw, got, err)
		}
	}
	for _, bad := range []string{"", "a:b", "has space", strings.Repeat("x", 33)} {
		if _, err := normalizeTrait(bad); err == nil {
			t.Errorf("normalizeTrait(%q) accepted", bad)
		}
	}
}
