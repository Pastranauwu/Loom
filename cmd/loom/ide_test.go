package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoloCavemanApagaLosDemasPlugins(t *testing.T) {
	f := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(f, []byte(`{"enabledPlugins":{"caveman@caveman":true,"ponytail@ponytail":true,"viejo@x":false}}`), 0o644)
	got := soloCaveman(f)
	if !strings.Contains(got, `"ponytail@ponytail":false`) || strings.Contains(got, "caveman@") || strings.Contains(got, "viejo@") {
		t.Fatalf("soloCaveman = %q", got)
	}
	if !strings.Contains(got, `"disableClaudeAiConnectors":true`) {
		t.Fatalf("los conectores de claude.ai siguen encendidos: %q", got)
	}
	// sin plugins que apagar, los conectores se apagan igual
	if got := soloCaveman(filepath.Join(t.TempDir(), "no-existe.json")); !strings.Contains(got, `"disableClaudeAiConnectors":true`) || strings.Contains(got, "enabledPlugins") {
		t.Fatalf("sin settings = %q", got)
	}
}
