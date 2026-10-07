package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

func TestScanPublishesOwnAndCuratedNames(t *testing.T) {
	h, _ := newTestHost(t, chrome, terminal, notes)
	names := mustNames(t, h)
	for spoken, id := range map[string]string{
		"google chrome": chrome.BundleID,
		"chrome":        chrome.BundleID,
		"terminal":      terminal.BundleID,
		"notes":         notes.BundleID,
	} {
		if names[spoken] != id {
			t.Errorf("names[%q] = %q, want %q", spoken, names[spoken], id)
		}
	}
	// Ghostty is trait-only: no name, no row.
	for _, a := range h.getApps() {
		if a.ID == "com.mitchellh.ghostty" {
			t.Error("a trait-only entry became an app")
		}
	}
}

// A curated app the scan did not find is still published, so a partial scan
// does not take away "chrome".
func TestCuratedAppPublishedWhenNotScanned(t *testing.T) {
	h, _ := newTestHost(t, notes)
	if mustNames(t, h)["chrome"] != chrome.BundleID {
		t.Error("curated chrome missing when the scan did not report it")
	}
}

// Traits come from the curated file, trait-only entries included, and are
// published even when the scan fails.
func TestTraitsPublishedEvenWhenScanFails(t *testing.T) {
	f := newFake()
	f.scanErr = errors.New("platform not ready")
	core := filepath.Join(t.TempDir(), "core_apps.json")
	os.WriteFile(core, []byte(testCore), 0o644)
	h := newHost(f, core, "darwin")
	if err := h.scanAndPublish(); err == nil {
		t.Fatal("expected the scan error")
	}
	cat, _ := h.loadTraits()
	if got := cat.ByApp["com.mitchellh.ghostty"]; len(got) != 1 || got[0] != "terminal" {
		t.Errorf("ghostty traits = %v", got)
	}
	// And `apps` was not replaced with the curated set alone.
	if len(f.base[appsCollection]) != 0 {
		t.Errorf("a failed scan published %d app names", len(f.base[appsCollection]))
	}
}

// A failed scan must not wipe the apps a previous good scan published.
func TestFailedRescanKeepsLastGoodApps(t *testing.T) {
	h, f := newTestHost(t, chrome, notes)
	f.scanErr = errors.New("transient")
	if err := h.scanAndPublish(); err == nil {
		t.Fatal("expected the scan error")
	}
	if mustNames(t, h)["notes"] != notes.BundleID {
		t.Error("a failed rescan removed apps from the last good scan")
	}
}

func TestScanWithRetryRecovers(t *testing.T) {
	f := newFake(notes)
	f.scanErr = errors.New("slow start")
	core := filepath.Join(t.TempDir(), "core_apps.json")
	os.WriteFile(core, []byte(testCore), 0o644)
	h := newHost(f, core, "darwin")
	sleeps := 0
	h.scanWithRetry(func(time.Duration) {
		sleeps++
		if sleeps == 2 {
			f.scanErr = nil
		}
	})
	if mustNames(t, h)["notes"] != notes.BundleID {
		t.Error("retry did not publish once the scan succeeded")
	}
	if sleeps != 2 {
		t.Errorf("slept %d times, want 2", sleeps)
	}
}

// The curated ids for the running OS are the ones used: on Linux, chrome is
// google-chrome, and macOS-only entries do not apply.
func TestCoreForOS(t *testing.T) {
	var core []coreApp
	if err := json.Unmarshal([]byte(testCore), &core); err != nil {
		t.Fatal(err)
	}
	linux := coreForOS(core, "linux")
	if len(linux) != 2 || linux[0].ID != "google-chrome" || linux[1].ID != "gnome-terminal" {
		t.Fatalf("linux core = %+v", linux)
	}
	if got := coreForOS(core, "windows"); len(got) != 0 {
		t.Errorf("windows core = %+v, want none", got)
	}
}

// A name two apps share goes to the first by name, and the clash is
// reported rather than settled by write order.
func TestAppRecordsClash(t *testing.T) {
	apps := []appEntry{
		{Name: "Code", ID: "com.microsoft.VSCode", Aliases: []string{"code"}},
		{Name: "Code Editor", ID: "com.example.code", Aliases: []string{"code", "code editor"}},
	}
	records, clashes := appRecords(apps)
	byName := map[string]string{}
	for _, r := range records {
		byName[r.Spoken] = r.AppID
	}
	if byName["code"] != "com.microsoft.VSCode" || byName["code editor"] != "com.example.code" {
		t.Errorf("records = %v", byName)
	}
	if len(clashes) != 1 || !strings.Contains(clashes[0], `"code"`) {
		t.Errorf("clashes = %v", clashes)
	}
}

// Every alias record carries the app's display name, so a plugin listing apps
// can show "Google Chrome" rather than the spoken "chrome".
func TestAppRecordsCarryTheDisplayName(t *testing.T) {
	records, _ := appRecords([]appEntry{
		{Name: "Google Chrome", ID: "com.google.Chrome", Aliases: []string{"google chrome", "chrome"}},
	})
	if len(records) != 2 {
		t.Fatalf("records = %v", records)
	}
	for _, r := range records {
		if r.Name != "Google Chrome" {
			t.Errorf("%q has name %q", r.Spoken, r.Name)
		}
	}
}

// An app focused that the registry has never seen triggers a rescan — the
// usual reason is that it was just installed — at most once a minute.
func TestUnknownFocusRescans(t *testing.T) {
	h, f := newTestHost(t, chrome)
	f.installed = append(f.installed, notes)
	clock := time.Now()
	h.now = func() time.Time { return clock }
	focus := func(id string) {
		raw, _ := json.Marshal(map[string]string{"bundle_id": id})
		h.onAppFocused(raw)
	}
	clock = clock.Add(2 * time.Minute)
	start := f.scans
	focus(chrome.BundleID)
	if f.scans != start {
		t.Fatal("a known app triggered a rescan")
	}
	focus(notes.BundleID)
	if f.scans != start+1 || mustNames(t, h)["notes"] != notes.BundleID {
		t.Fatalf("unknown app: scans %d→%d, notes=%q", start, f.scans, mustNames(t, h)["notes"])
	}
	// A helper the scan never reports: quiet for a minute after the last scan,
	// then one more try.
	clock = clock.Add(30 * time.Second)
	focus("com.example.helper")
	if f.scans != start+1 {
		t.Errorf("rescanned within a minute of the last scan")
	}
	clock = clock.Add(31 * time.Second)
	focus("com.example.helper")
	if f.scans != start+2 {
		t.Errorf("did not rescan a minute later")
	}
}

// The shipped curated file: well-formed, every entry does something, ids are
// unique per OS, trait-only entries say they are unverified, and every trait
// has a reader.
func TestCoreAppsFileIsWellFormed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "core_apps.json"))
	if err != nil {
		t.Fatal(err)
	}
	var core []coreApp
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&core); err != nil {
		t.Fatalf("parse core_apps.json: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range core {
		if c.Name == "" || len(c.IDs) == 0 {
			t.Errorf("entry missing name or ids: %+v", c)
		}
		for os, ids := range c.IDs {
			if os != "macos" && os != "linux" && os != "windows" {
				t.Errorf("%s: unknown OS %q", c.Name, os)
			}
			for _, id := range ids {
				key := os + "/" + id
				if seen[key] {
					t.Errorf("duplicate id %s", key)
				}
				seen[key] = true
			}
		}
		if len(c.Aliases) == 0 && len(c.Traits) == 0 {
			t.Errorf("%s has neither names nor traits, so it does nothing", c.Name)
		}
		if len(c.Aliases) == 0 && c.Note == "" {
			t.Errorf("%s is trait-only but undocumented: say its ids are unverified", c.Name)
		}
		// The taxonomy is deliberately narrow: only traits a consumer reads.
		for _, tr := range c.Traits {
			if tr != "terminal" && tr != "chat" {
				t.Errorf("%s: trait %q has no reader", c.Name, tr)
			}
		}
	}
}

// The apps voice used to hard-code must all still be classified on macOS.
func TestCuratedTraitsKeepTheirClassification(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "core_apps.json"))
	if err != nil {
		t.Fatal(err)
	}
	var core []coreApp
	if err := json.Unmarshal(raw, &core); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tr := range appTraits(coreForOS(core, "macos")) {
		got[tr.AppID] = tr.Trait
	}
	for id, want := range map[string]string{
		"com.tinyspeck.slackmacgap": "chat", "com.hnc.Discord": "chat", "com.apple.MobileSMS": "chat",
		"net.whatsapp.WhatsApp": "chat", "ru.keepcoder.Telegram": "chat", "org.telegram.desktop": "chat",
		"org.whispersystems.signal-desktop": "chat", "com.apple.Terminal": "terminal",
		"com.googlecode.iterm2": "terminal", "com.mitchellh.ghostty": "terminal", "dev.warp.Warp-Stable": "terminal",
		"net.kovidgoyal.kitty": "terminal", "org.alacritty": "terminal", "com.github.wez.wezterm": "terminal",
	} {
		if got[id] != want {
			t.Errorf("%s trait = %q, want %q", id, got[id], want)
		}
	}
}

var _ = branchkit.InstalledApp{}
