package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// The two shared collections. Bare names because other plugins read them:
// `<apps>` captures from one, voice reads the other. The platform reserves
// `plugin.*` for collections nobody else may read.
const (
	appsCollection   = "apps"
	traitsCollection = "app_traits"
)

// appEntry is one application as this plugin publishes it.
type appEntry struct {
	Name string
	// ID is what the platform on THIS OS calls the app: a bundle identifier on
	// macOS, a desktop entry on Linux ("google-chrome"), an executable's
	// lowercased stem on Windows ("chrome"). It is what launch_app takes and
	// what app.focused reports.
	ID      string
	Aliases []string // lowercase; the app's own name first
	Traits  []string
}

// coreApp is one entry of core_apps.json: an application this plugin knows
// something about before any scan — names people call it, what kind of app
// it is, and what each OS calls it.
type coreApp struct {
	Name string `json:"name"`
	// IDs maps an OS ("macos", "linux", "windows") to what that OS's platform
	// calls the app. Every OS is spelled out, macOS included, so no OS's
	// identifier is the default the others are mapped from. An entry with no
	// id for the running OS does not apply on it.
	IDs     map[string][]string `json:"ids"`
	Aliases []string            `json:"aliases,omitempty"`
	Traits  []string            `json:"traits,omitempty"`
	// Note documents an entry nobody here has run — JSON has no comments, so
	// unverified ids say so in the data. Never read by code.
	Note string `json:"note,omitempty"`
}

// osKey names the running OS the way core_apps.json does.
func osKey(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

// loadCoreApps reads the curated apps and keeps those that name an app on
// this OS, one entry per id, each carrying that OS's id.
func (h *Host) loadCoreApps() []appEntry {
	data, err := os.ReadFile(h.coreFile)
	if err != nil {
		branchkit.Logf("apps", "read %s: %v", h.coreFile, err)
		return nil
	}
	var core []coreApp
	if err := json.Unmarshal(data, &core); err != nil {
		branchkit.Logf("apps", "parse %s: %v", h.coreFile, err)
		return nil
	}
	return coreForOS(core, osKey(h.goos))
}

func coreForOS(core []coreApp, os string) []appEntry {
	var out []appEntry
	for _, c := range core {
		for _, id := range c.IDs[os] {
			out = append(out, appEntry{
				Name:    c.Name,
				ID:      id,
				Aliases: normalizeNames(c.Aliases),
				Traits:  normalizeNames(c.Traits),
			})
		}
	}
	return out
}

// normalizeNames lowercases, trims and de-duplicates, keeping order.
func normalizeNames(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// mergeApps joins the scan with the curated apps, matching on id.
//
// A scanned app gets its own name plus the curated names for its id. A
// curated app with names that the scan did not find is still published: a
// failed or partial scan must not take away "chrome", and launching an app
// that is not installed fails loudly rather than mishearing. A curated
// entry with NO names is trait-only — it says what kind of app an id is and
// nothing else — so it adds no row and no vocabulary, and its traits are
// published from the curated list directly (see appTraits).
func mergeApps(scanned []branchkit.InstalledApp, core []appEntry) []appEntry {
	var out []appEntry
	idx := map[string]int{}
	for _, a := range scanned {
		if a.BundleID == "" {
			continue
		}
		key := strings.ToLower(a.BundleID)
		if _, dup := idx[key]; dup {
			continue
		}
		idx[key] = len(out)
		out = append(out, appEntry{Name: a.Name, ID: a.BundleID, Aliases: normalizeNames([]string{a.Name})})
	}
	for _, c := range core {
		key := strings.ToLower(c.ID)
		if i, ok := idx[key]; ok {
			out[i].Aliases = normalizeNames(append(out[i].Aliases, c.Aliases...))
			out[i].Traits = c.Traits
			continue
		}
		if len(c.Aliases) == 0 {
			continue
		}
		idx[key] = len(out)
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// appRecord is one name you can say for one app.
type appRecord struct {
	Spoken string `json:"spoken"`
	AppID  string `json:"app_id"`
}

// appRecords flattens apps into one record per spoken name. A name two apps
// share goes to the first in name order, deterministically, and the clash is
// reported: a record id is the spoken name, so one name can only mean one
// app, and a clash resolved by whichever entry happened to be written last
// would change meaning between scans.
func appRecords(apps []appEntry) (records []appRecord, clashes []string) {
	owner := map[string]string{}
	for _, a := range apps {
		for _, alias := range a.Aliases {
			if prev, taken := owner[alias]; taken {
				if prev != a.ID {
					clashes = append(clashes, fmt.Sprintf("%q names %s and %s; kept %s", alias, prev, a.ID, prev))
				}
				continue
			}
			owner[alias] = a.ID
			records = append(records, appRecord{Spoken: alias, AppID: a.ID})
		}
	}
	return records, clashes
}

// traitRecord is one (trait, app) pair.
//
// ONE RECORD PER PAIR, keyed "<trait>:<app_id>", rather than one record per
// app holding a list: the `apps` collection is keyed by spoken name, so there
// is no per-app record for a list to live on, and a pair with its own id is
// something the user band can suppress on its own.
type traitRecord struct {
	Key   string `json:"key"`
	Trait string `json:"trait"`
	AppID string `json:"app_id"`
}

func traitRecordID(trait, appID string) string { return trait + ":" + appID }

// appTraits lists the trait records the curated apps declare for this OS.
// Read from the curated list, not the merged one: the scan cannot know what
// kind of app something is, and trait-only entries are not in the merged
// list at all.
func appTraits(core []appEntry) []traitRecord {
	var out []traitRecord
	seen := map[string]bool{}
	for _, c := range core {
		for _, t := range c.Traits {
			id := traitRecordID(t, c.ID)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, traitRecord{Key: id, Trait: t, AppID: c.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// scanAndPublish scans the installed apps, merges the curated ones, and
// publishes both collections. A failed scan publishes NOTHING to `apps`:
// it is a whole-scope replace, so publishing the curated set alone would
// delete every real app the last good scan found.
func (h *Host) scanAndPublish() error {
	h.scanMu.Lock()
	h.lastScan = h.now()
	h.scanMu.Unlock()

	core := h.loadCoreApps()
	// Traits do not depend on the scan, so they are published either way.
	h.publishTraits(appTraits(core))

	scanned, err := h.plugin.NativeInstalledApps()
	if err != nil {
		return fmt.Errorf("installed apps: %w", err)
	}
	apps := mergeApps(scanned, core)
	known := make(map[string]bool, len(apps))
	for _, a := range apps {
		known[strings.ToLower(a.ID)] = true
	}
	h.appsMu.Lock()
	h.apps = apps
	h.known = known
	h.appsMu.Unlock()
	return h.publishApps(apps)
}

func (h *Host) publishApps(apps []appEntry) error {
	records, clashes := appRecords(apps)
	for _, c := range clashes {
		branchkit.Logf("apps", "name clash: %s", c)
	}
	entries := make([]branchkit.CollectionPutEntry, 0, len(records))
	for _, r := range records {
		raw, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("marshal %q: %w", r.Spoken, err)
		}
		entries = append(entries, branchkit.CollectionPutEntry{ID: r.Spoken, Payload: raw})
	}
	if _, err := h.plugin.Replace(appsCollection, entries, branchkit.ScopeCollection()); err != nil {
		return fmt.Errorf("replace %s: %w", appsCollection, err)
	}
	branchkit.Logf("apps", "published %d names for %d apps", len(records), len(apps))
	return nil
}

func (h *Host) publishTraits(traits []traitRecord) {
	entries := make([]branchkit.CollectionPutEntry, 0, len(traits))
	for _, t := range traits {
		raw, err := json.Marshal(t)
		if err != nil {
			branchkit.Logf("apps", "%s: marshal %s: %v", traitsCollection, t.Key, err)
			return
		}
		entries = append(entries, branchkit.CollectionPutEntry{ID: t.Key, Payload: raw})
	}
	if _, err := h.plugin.Replace(traitsCollection, entries, branchkit.ScopeCollection()); err != nil {
		branchkit.Logf("apps", "replace %s: %v", traitsCollection, err)
	}
}

// scanRetryDelays is the backoff after a failed first scan. Front-loaded,
// because the failure it exists for is a slow platform at startup, and
// bounded, because a permanently broken scan should settle rather than spin.
var scanRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}

// scanWithRetry runs the first scan and, if it fails, retries in the
// background until one succeeds or the backoff runs out.
func (h *Host) scanWithRetry(sleep func(time.Duration)) {
	err := h.scanAndPublish()
	for i := 0; err != nil && i < len(scanRetryDelays); i++ {
		branchkit.Logf("apps", "app scan failed (%v); retry %d/%d in %s", err, i+1, len(scanRetryDelays), scanRetryDelays[i])
		sleep(scanRetryDelays[i])
		err = h.scanAndPublish()
	}
	if err != nil {
		branchkit.Logf("apps", "app scan never succeeded: %v", err)
	}
}

// rescanInterval bounds how often a focus on an unknown app may trigger a
// rescan, so an app the scan can never see (a helper, a tray-only process)
// costs one scan a minute at most, not one per focus change.
const rescanInterval = time.Minute

// onAppFocused rescans when the focused app is not one the registry knows:
// the usual reason is that it was installed since the last scan, and without
// this a new app cannot be opened by name until BranchKit restarts.
func (h *Host) onAppFocused(params json.RawMessage) {
	var ev struct {
		BundleID string `json:"bundle_id"`
	}
	if err := json.Unmarshal(params, &ev); err != nil || ev.BundleID == "" {
		return
	}
	h.appsMu.Lock()
	known := h.known[strings.ToLower(ev.BundleID)]
	h.appsMu.Unlock()
	if known {
		return
	}
	h.scanMu.Lock()
	recent := h.now().Sub(h.lastScan) < rescanInterval
	h.scanMu.Unlock()
	if recent {
		return
	}
	branchkit.Logf("apps", "focused app %s is not in the registry; rescanning", ev.BundleID)
	if err := h.scanAndPublish(); err != nil {
		branchkit.Logf("apps", "rescan: %v", err)
	}
}

// getApps returns a snapshot of what this plugin publishes.
func (h *Host) getApps() []appEntry {
	h.appsMu.Lock()
	defer h.appsMu.Unlock()
	return append([]appEntry(nil), h.apps...)
}
