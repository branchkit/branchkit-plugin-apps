package main

import (
	"strings"
	"testing"

	"github.com/branchkit/plugin-sdk-go"
)

func TestRenderAppsTab(t *testing.T) {
	h, _ := newTestHost(t, chrome, terminal)
	html, err := h.renderAppsTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Google Chrome",
		`Stop &#34;chrome&#34; opening Google Chrome`,
		`Remove the trait &#34;terminal&#34; from Terminal`,
		"terminal (2)", // Terminal and the trait-only Ghostty
		"Move the pointer to an app you open",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("apps tab missing %q", want)
		}
	}
}

func TestRenderAppsTab_Search(t *testing.T) {
	h, _ := newTestHost(t, chrome, terminal)
	html, err := h.renderAppsTab(&branchkit.RenderSettingsRequest{Search: "apple.term"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, ">Terminal<") || strings.Contains(html, ">Google Chrome<") {
		t.Error("search by app id did not filter the rows")
	}
}

// A name the user added for an app id the scan does not report still gets a
// row, so the edit can be seen and undone.
func TestAppRows_UserOnlyApp(t *testing.T) {
	rows := appRows(nil, map[string]string{"thing": "com.example.thing"}, traitCatalog{}, "")
	if len(rows) != 1 || rows[0].ID != "com.example.thing" || !rows[0].Enabled {
		t.Errorf("rows = %+v", rows)
	}
}
