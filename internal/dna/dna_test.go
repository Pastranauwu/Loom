package dna

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBloqueEsDeterministaYRespetaElOrden(t *testing.T) {
	dir := t.TempDir()
	a, desc := Bloque(dir, []string{"zero-deps", "crash-early", "nada"})
	if !strings.HasPrefix(a, "Project DNA") || strings.Index(a, "zero-deps") > strings.Index(a, "crash-early") {
		t.Errorf("bloque:\n%s", a)
	}
	if len(desc) != 1 || desc[0] != "nada" {
		t.Errorf("desconocidos = %v", desc)
	}
	if b, _ := Bloque(dir, []string{"zero-deps", "crash-early"}); b != a {
		t.Error("mismo input, mismos bytes")
	}
	if b, _ := Bloque(dir, nil); b != "" {
		t.Error("sin dna no agrega nada")
	}
}

func TestArquetipoPropioPisaAlIncorporado(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dna"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dna", "zero-deps.md"), []byte("# Permitido solo lo del lockfile\nNada nuevo."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dna", "mio.md"), []byte("Siempre tabs."), 0o644); err != nil {
		t.Fatal(err)
	}
	var propios []string
	for _, a := range Lista(dir) {
		if a.Propio {
			propios = append(propios, a.Nombre)
		}
	}
	if strings.Join(propios, ",") != "mio,zero-deps" {
		t.Errorf("propios = %v", propios)
	}
	if b, _ := Bloque(dir, []string{"zero-deps"}); !strings.Contains(b, "lockfile") || strings.Contains(b, "third-party") {
		t.Errorf("bloque:\n%s", b)
	}
}
