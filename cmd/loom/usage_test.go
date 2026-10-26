package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/ui"
)

func TestUsageIncluyeCostoDelArquitecto(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".loom", "runs", "T-001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := "=== esqueleto 1 · 100 tokens de entrada · 200 de salida · $1.250\n--- stderr\n"
	if err := os.WriteFile(filepath.Join(dir, "esqueleto-1.log"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "esqueleto-2.log"), []byte("=== esqueleto 2 · 20 tokens de entrada · 30 de salida · $0.001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "esqueleto-2.usage.json"), []byte(`{"input":20,"output":30,"cache_read":40,"cache_write":50,"cost_usd":2.5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "examinador-usage.jsonl"), []byte(`{"entrada":10,"salida":20,"costo_usd":0.5}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	anterior := out
	out = ui.New(&b, false)
	t.Cleanup(func() { out = anterior })
	if err := mostrarGastoPorTarea(root); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"T-001/arq", "T-001/exam", "4.250", "precio de lista"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("falta %q en reporte:\n%s", want, b.String())
		}
	}
}
