package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/branchkit/plugin-sdk-go"
)

// The user's edits: names added and removed, apps turned off, traits added
// and removed.
//
// Every edit is a platform OVERRIDE in the user band (`_user`), never a write
// to this plugin's own records. The user band composes over what this plugin
// publishes, survives every republish, and is visible and undoable in the
// Collections page like every other edit.
//
// Every edit RESTORES FIRST. `restore` drops all user-band state for one id,
// so removing a name the user added deletes it, and putting back a shipped
// name the user removed clears the suppression instead of storing a private
// copy that would stop tracking what ships. The user band then holds only
// what genuinely differs from what this plugin publishes.
//
// Everything a screen shows is read back from the COMPOSED collection
// (loadNames, loadTraits), never from this plugin's memory: memory holds
// only what this plugin publishes, and showing it as the user's state is how
// a restart used to bring back names the user had removed and hide the ones
// they had added.

// loadNames reads `apps` as it stands, the user's edits applied, as spoken
// name → app id. A failed read is an error, not an empty map.
func (h *Host) loadNames() (map[string]string, error) {
	records, err := h.plugin.ListAll(appsCollection)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", appsCollection, err)
	}
	out := make(map[string]string, len(records))
	for _, rec := range records {
		var r appRecord
		if err := json.Unmarshal(rec.Payload, &r); err != nil || r.Spoken == "" || r.AppID == "" {
			continue
		}
		out[r.Spoken] = r.AppID
	}
	return out, nil
}

// namesByApp inverts loadNames: app id → its spoken names, sorted.
func namesByApp(names map[string]string) map[string][]string {
	out := map[string][]string{}
	for spoken, id := range names {
		out[id] = append(out[id], spoken)
	}
	for id := range out {
		sort.Strings(out[id])
	}
	return out
}

func spokenForm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// addAppName makes `alias` a name for the app. A name that already means a
// different app is refused, naming it: one of the two would become
// unreachable by voice.
func (h *Host) addAppName(appID, rawAlias string) error {
	alias := spokenForm(rawAlias)
	if alias == "" {
		return fmt.Errorf("a name can't be empty")
	}
	if appID == "" {
		return fmt.Errorf("no app given")
	}
	names, err := h.loadNames()
	if err != nil {
		return err
	}
	if other, ok := names[alias]; ok {
		if other == appID {
			return nil
		}
		return fmt.Errorf("%q already names %s", alias, h.displayName(other))
	}
	if err := userBandOverride(h.plugin, appsCollection, "restore", alias, nil); err != nil {
		branchkit.Logf("apps", "name restore %q (may be a no-op): %v", alias, err)
	}
	if names, err := h.loadNames(); err == nil && names[alias] == appID {
		return nil
	}
	if err := userBandOverride(h.plugin, appsCollection, "add", "", appRecord{Spoken: alias, AppID: appID}); err != nil {
		return fmt.Errorf("could not add %q: %w", alias, err)
	}
	return nil
}

// removeAppName stops `alias` naming the app.
func (h *Host) removeAppName(appID, rawAlias string) error {
	alias := spokenForm(rawAlias)
	names, err := h.loadNames()
	if err != nil {
		return err
	}
	if names[alias] != appID {
		return nil
	}
	if err := userBandOverride(h.plugin, appsCollection, "restore", alias, nil); err != nil {
		branchkit.Logf("apps", "name restore %q (may be a no-op): %v", alias, err)
	}
	// Gone: it was the user's own, and restoring deleted it.
	if names, err := h.loadNames(); err == nil && names[alias] != appID {
		return nil
	}
	// Still there, so this plugin publishes it. Suppress it.
	if err := userBandOverride(h.plugin, appsCollection, "remove", alias, nil); err != nil {
		return fmt.Errorf("could not remove %q: %w", alias, err)
	}
	return nil
}

// setAppEnabled turns an app on or off. An app is on while it has a name you
// can say: off removes every name, on puts back the names this plugin
// publishes for it. Names the user added are removed with the rest, and do
// not come back when the app is turned on again.
func (h *Host) setAppEnabled(appID string, enabled bool) error {
	if !enabled {
		names, err := h.loadNames()
		if err != nil {
			return err
		}
		for _, alias := range namesByApp(names)[appID] {
			if err := h.removeAppName(appID, alias); err != nil {
				return err
			}
		}
		return nil
	}
	for _, a := range h.getApps() {
		if a.ID != appID {
			continue
		}
		for _, alias := range a.Aliases {
			if err := userBandOverride(h.plugin, appsCollection, "restore", alias, nil); err != nil {
				branchkit.Logf("apps", "name restore %q (may be a no-op): %v", alias, err)
			}
		}
		return nil
	}
	return fmt.Errorf("%s is not an app this plugin knows", appID)
}

// displayName is the app's name for messages, falling back to its id.
func (h *Host) displayName(appID string) string {
	for _, a := range h.getApps() {
		if a.ID == appID {
			return a.Name
		}
	}
	return appID
}

// --- Traits ---

// traitCatalog is one render's worth of trait state.
type traitCatalog struct {
	// ByApp is the COMPOSED view: shipped traits with the user's edits.
	ByApp map[string][]string
	// Counts feeds the "terminal (7)" labels on the add menu, the typo
	// backstop: a stray "terminl (1)" beside "terminal (7)" gives itself away
	// where the mistake is made.
	Counts map[string]int
	// Names is every trait that exists, sorted, for the add menu.
	Names []string
}

func (h *Host) loadTraits() (traitCatalog, error) {
	cat := traitCatalog{ByApp: map[string][]string{}, Counts: map[string]int{}}
	records, err := h.plugin.ListAll(traitsCollection)
	if err != nil {
		return cat, fmt.Errorf("read %s: %w", traitsCollection, err)
	}
	for _, rec := range records {
		var t traitRecord
		if err := json.Unmarshal(rec.Payload, &t); err != nil || t.Trait == "" || t.AppID == "" {
			continue
		}
		cat.ByApp[t.AppID] = append(cat.ByApp[t.AppID], t.Trait)
		cat.Counts[t.Trait]++
	}
	for id := range cat.ByApp {
		sort.Strings(cat.ByApp[id])
	}
	for name := range cat.Counts {
		cat.Names = append(cat.Names, name)
	}
	sort.Strings(cat.Names)
	return cat, nil
}

func (h *Host) hasTrait(appID, trait string) bool {
	cat, err := h.loadTraits()
	if err != nil {
		return false
	}
	for _, t := range cat.ByApp[appID] {
		if t == trait {
			return true
		}
	}
	return false
}

// normalizeTrait lowercases and trims a trait name, and refuses one that
// cannot safely be half of a record id. The `:` ban is the important one: it
// separates trait from app id in traitRecordID. The rest is hygiene — trait
// names are compared across plugins, and two spellings of one idea is the
// failure this vocabulary is least able to detect.
func normalizeTrait(raw string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return "", fmt.Errorf("a trait needs a name")
	}
	if len(t) > 32 {
		return "", fmt.Errorf("trait names are at most 32 characters")
	}
	for _, r := range t {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return "", fmt.Errorf("trait names use letters, digits, hyphen and underscore only — %q is not allowed", string(r))
		}
	}
	return t, nil
}

func (h *Host) addTrait(appID, rawTrait string) error {
	trait, err := normalizeTrait(rawTrait)
	if err != nil {
		return err
	}
	if appID == "" {
		return fmt.Errorf("no app given")
	}
	id := traitRecordID(trait, appID)
	if err := userBandOverride(h.plugin, traitsCollection, "restore", id, nil); err != nil {
		branchkit.Logf("apps", "trait restore %q (may be a no-op): %v", id, err)
	}
	if h.hasTrait(appID, trait) {
		return nil
	}
	if err := userBandOverride(h.plugin, traitsCollection, "add", "", traitRecord{Key: id, Trait: trait, AppID: appID}); err != nil {
		return fmt.Errorf("could not add trait %q: %w", trait, err)
	}
	return nil
}

func (h *Host) removeTrait(appID, rawTrait string) error {
	trait, err := normalizeTrait(rawTrait)
	if err != nil {
		return err
	}
	id := traitRecordID(trait, appID)
	if err := userBandOverride(h.plugin, traitsCollection, "restore", id, nil); err != nil {
		branchkit.Logf("apps", "trait restore %q (may be a no-op): %v", id, err)
	}
	if !h.hasTrait(appID, trait) {
		return nil
	}
	if err := userBandOverride(h.plugin, traitsCollection, "remove", id, nil); err != nil {
		return fmt.Errorf("could not remove trait %q: %w", trait, err)
	}
	return nil
}

// userBandOverride relays one user gesture into the user band of one of this
// plugin's collections: by record id, or with a whole record to add.
func userBandOverride(p platform, collection, action, id string, record any) error {
	tenant := "_user"
	var idPtr *string
	if id != "" {
		idPtr = &id
	}
	var raw json.RawMessage
	if record != nil {
		b, err := json.Marshal(record)
		if err != nil {
			return err
		}
		raw = b
	}
	_, err := p.OverridesApply(branchkit.OverridesApplyRequest{Action: action, Collection: collection, Fields: raw, ID: idPtr, Tenant: &tenant})
	return err
}
