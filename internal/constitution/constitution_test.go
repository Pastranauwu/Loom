package constitution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ausente no es un error: la constitución es opcional y todo el mundo la
// carga al arrancar. Si Load devolviera error, correr sin ella frenaría.
func TestLoadSinArchivoDevuelveVacioSinError(t *testing.T) {
	root := t.TempDir()
	if Exists(root) {
		t.Fatal("Exists dijo que sí en un repo sin constitución")
	}
	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load sin archivo devolvió error: %v", err)
	}
	if c != "" {
		t.Errorf("Load sin archivo = %q, quiero vacío", c)
	}
}

func TestSaveYLoadDanLaVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	const contenido = "# Constitución del proyecto\n\n## Estructura de capas\ncmd importa internal, nunca al revés.\n"
	if err := Save(root, contenido); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists(root) {
		t.Error("Exists dijo que no después de Save")
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != contenido {
		t.Errorf("Load = %q, quiero %q", got, contenido)
	}
}

// El prompt lleva las secciones exactas porque el resto del flujo asume
// esa forma; si se reescriben los encabezados, esto avisa.
func TestPromptLlevaLasSeccionesYElContexto(t *testing.T) {
	p := Prompt("go", "go test ./...", "cmd/loom/main.go")
	for _, quiero := range []string{
		"Project language: go",
		"Test command: go test ./...",
		"cmd/loom/main.go",
		"## Layer structure",
		"## Style conventions",
		"## Forbidden patterns",
		"## Maximum file size",
	} {
		if !strings.Contains(p, quiero) {
			t.Errorf("el prompt no menciona %q", quiero)
		}
	}
}

// Sin datos de config no se inventan líneas vacías tipo "Lenguaje: ".
func TestPromptSinContextoNoDejaCamposHuecos(t *testing.T) {
	p := Prompt("", "", "")
	for _, sobra := range []string{"Project language:", "Test command:", "```"} {
		if strings.Contains(p, sobra) {
			t.Errorf("el prompt sin contexto trae %q", sobra)
		}
	}
}
