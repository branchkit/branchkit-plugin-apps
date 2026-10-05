package main

import (
	"sync"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// platform is every call this plugin makes on the platform, so tests can
// stand in for it. *branchkit.Plugin satisfies it.
type platform interface {
	NativeInstalledApps() ([]branchkit.InstalledApp, error)
	NativeLaunchApp(branchkit.NativeLaunchAppRequest) error
	NativeNewAppWindow(branchkit.NativeNewAppWindowRequest) (bool, error)
	NativeOpenTarget(branchkit.NativeOpenTargetRequest) error
	NativeFrontmostApp() (*branchkit.NativeFrontmostAppResponse, error)
	NativeAppWindows(branchkit.NativeAppWindowsRequest) ([]branchkit.WindowDetail, error)
	NativeCursor() (*branchkit.NativeCursorResponse, error)
	NativeWarpCursor(branchkit.NativeWarpCursorRequest) error
	ListAll(name string) ([]branchkit.CollectionRecord, error)
	Replace(name string, entries []branchkit.CollectionPutEntry, scope branchkit.ReplaceScope, opts ...branchkit.ReplaceOption) (branchkit.ReplaceResult, error)
	OverridesApply(branchkit.OverridesApplyRequest) (*branchkit.OverridesApplyResponse, error)
}

// Host is what every handler needs. Handlers are methods on it, so a
// handler's dependencies are visible in its signature, and each mutex sits
// beside the data it guards.
type Host struct {
	plugin       platform
	configMirror *branchkit.SettingsMirror[AppsConfig]
	// coreFile is where the curated apps are read from: beside the plugin
	// binary in production, a fixture in tests.
	coreFile string
	goos     string

	// apps is what this plugin PUBLISHES: the scanned apps with the curated
	// names merged in. It is not what the user sees or says — that is the
	// collection with the user's edits composed over it, read per use.
	appsMu sync.Mutex
	apps   []appEntry
	known  map[string]bool // app ids in apps, for the unknown-focus rescan

	scanMu   sync.Mutex
	lastScan time.Time
	now      func() time.Time // the clock the rescan limit reads; a test sets it
}

func newHost(p platform, coreFile, goos string) *Host {
	return &Host{plugin: p, coreFile: coreFile, goos: goos, known: map[string]bool{}, now: time.Now}
}
