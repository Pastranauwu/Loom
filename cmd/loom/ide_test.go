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
	if soloCaveman(filepath.Join(t.TempDir(), "no-existe.json")) != "" {
		t.Fatal("sin settings no debe agregar --settings")
	}
}
