package main

import "github.com/branchkit/plugin-sdk-go"

// AppsConfig rides the settings preset: plugin.json declares the fields with
// defaults, the platform composes the view, and this plugin only reads it. A
// plugin never writes its own settings collection; the Apps tab's switch
// relays the user's gesture into the user band through the mirror.
type AppsConfig struct {
	PointerToOpenedApp bool `json:"pointer_to_opened_app"`
}

const configCollection = "plugin.apps.config"

func (h *Host) initConfig(p *branchkit.Plugin) {
	h.configMirror = branchkit.Settings[AppsConfig](p, configCollection)
}

// LoadConfig returns the composed settings, or the manifest's defaults
// before the mirror is ready (plugin.json is the authoritative copy).
func (h *Host) LoadConfig() AppsConfig {
	if h.configMirror != nil && h.configMirror.Ready() {
		return h.configMirror.Get()
	}
	return AppsConfig{}
}

func (h *Host) setConfigField(key string, value any) error {
	if h.configMirror == nil {
		return nil
	}
	return h.configMirror.SetUser(key, value)
}
