package main

import (
	"strings"

	"github.com/branchkit/plugin-sdk-go"
	"github.com/branchkit/plugin-sdk-go/ui"
)

// appRow is one row of the Apps tab, read from the composed collections.
type appRow struct {
	Name    string
	ID      string
	Names   []string // what you can say for it now
	Traits  []string
	Enabled bool // has at least one name
}

// appRows joins what this plugin publishes with what the collections hold
// now. The names, traits and on/off state shown are the composed ones — the
// user's edits included, after a restart as much as before. An app known
// only from a user's edit (a name added for an id the scan does not report)
// still gets a row, so the edit can be seen and undone.
func appRows(apps []appEntry, names map[string]string, traits traitCatalog, search string) []appRow {
	byApp := namesByApp(names)
	seen := map[string]bool{}
	var rows []appRow
	add := func(name, id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if search != "" && !strings.Contains(strings.ToLower(name), search) && !strings.Contains(strings.ToLower(id), search) {
			return
		}
		rows = append(rows, appRow{Name: name, ID: id, Names: byApp[id], Traits: traits.ByApp[id], Enabled: len(byApp[id]) > 0})
	}
	for _, a := range apps {
		add(a.Name, a.ID)
	}
	for id := range byApp {
		add(id, id)
	}
	return rows
}

func (h *Host) renderAppsTab(req *branchkit.RenderSettingsRequest) (string, error) {
	names, err := h.loadNames()
	if err != nil {
		branchkit.Logf("apps", "apps tab: %v", err)
	}
	traits, err := h.loadTraits()
	if err != nil {
		branchkit.Logf("apps", "apps tab: %v", err)
	}
	search := ""
	if req != nil {
		search = strings.ToLower(strings.TrimSpace(req.Search))
	}
	rows := appRows(h.getApps(), names, traits, search)
	return branchkit.RenderComponent(Apps(rows, h.LoadConfig().PointerToOpenedApp, traits))
}

// --- Settings commands ---

type appNameRequest struct {
	AppID string `json:"app_id"`
	Name  string `json:"name"`
}

func (h *Host) handleNameAdd(req *appNameRequest) error { return h.addAppName(req.AppID, req.Name) }
func (h *Host) handleNameRemove(req *appNameRequest) error {
	return h.removeAppName(req.AppID, req.Name)
}

type appEnabledRequest struct {
	AppID   string `json:"app_id"`
	Enabled bool   `json:"enabled"`
}

func (h *Host) handleSetEnabled(req *appEnabledRequest) error {
	return h.setAppEnabled(req.AppID, req.Enabled)
}

type appTraitRequest struct {
	AppID string `json:"app_id"`
	Trait string `json:"trait"`
}

func (h *Host) handleTraitAdd(req *appTraitRequest) error { return h.addTrait(req.AppID, req.Trait) }
func (h *Host) handleTraitRemove(req *appTraitRequest) error {
	return h.removeTrait(req.AppID, req.Trait)
}

type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Host) handleSetPointerToOpenedApp(req *setEnabledRequest) error {
	return h.setConfigField("pointer_to_opened_app", req.Enabled)
}

// traitAddPost builds the @post for adding a trait, where the trait is a JS
// EXPRESSION — `evt.target.value` from the select, or `$newTrait` from the
// text box — so the two controls share it.
func traitAddPost(appID, traitExpr string) string {
	return branchkit.MethodPost("trait_add", ui.Args("app_id", appID, "trait", ui.Expr(traitExpr)))
}
