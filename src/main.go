package main

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"runtime"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

//go:embed settings.css
var settingsCSS string

func main() {
	p := branchkit.NewPlugin()
	h := newHost(p, filepath.Join(branchkit.PluginDir(), "core_apps.json"), runtime.GOOS)
	h.initConfig(p)

	// Registrars come from actions_gen.go, generated from plugin.json, so no
	// action string is spelled here and a params type cannot drift from the
	// manifest.
	HandleLaunch(p, h.handleLaunch)
	HandleNewWindow(p, h.handleNewWindow)
	HandleOpen(p, h.handleOpen)

	p.SettingsCSS(settingsCSS)
	p.SettingsTab("apps", h.renderAppsTab)
	branchkit.HandleCommand(p, "name_add", h.handleNameAdd)
	branchkit.HandleCommand(p, "name_remove", h.handleNameRemove)
	branchkit.HandleCommand(p, "set_enabled", h.handleSetEnabled)
	branchkit.HandleCommand(p, "trait_add", h.handleTraitAdd)
	branchkit.HandleCommand(p, "trait_remove", h.handleTraitRemove)
	branchkit.HandleCommand(p, "set_pointer_to_opened_app", h.handleSetPointerToOpenedApp)

	// The registry tracks what is installed: scanned once the platform is
	// reachable (retrying while it is slow to start), again on wake, and
	// again when an app the registry has never seen takes focus — usually
	// one installed since the last scan.
	p.OnReady(func() { go h.scanWithRetry(time.Sleep) })
	p.On("_platform.system.did_wake", func(json.RawMessage) {
		if err := h.scanAndPublish(); err != nil {
			branchkit.Logf("apps", "rescan on wake: %v", err)
		}
	})
	p.On("_platform.app.focused", func(params json.RawMessage) { h.onAppFocused(params) })

	p.Run()
}
