package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestIntegrationCheckboxesRequireExplicitSelection(t *testing.T) {
	script, err := uiFiles.ReadFile("ui/integrations.js")
	if err != nil {
		t.Fatal(err)
	}
	checkboxes := regexp.MustCompile(`<input\b[^>]*class="integration-check"[^>]*>`).FindAll(script, -1)
	if len(checkboxes) == 0 {
		t.Fatal("missing integration checkbox template")
	}
	for _, checkbox := range checkboxes {
		if strings.Contains(string(checkbox), "checked") {
			t.Fatalf("integration checkbox must not be checked by default: %s", checkbox)
		}
	}
}
