package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestConfirmarPruebas(t *testing.T) {
	casos := []struct {
		nombre    string
		detectado string
		respuesta string
		quiere    string
	}{
		{"enter acepta lo detectado", "make test", "\n", "make test"},
		{"corrige lo detectado", "make test", "pytest -q\n", "pytest -q"},
		{"eof acepta lo detectado", "make test", "", "make test"},
		{"declara lo no detectado", "", "go test ./...\n", "go test ./..."},
		{"enter deja vacío lo no detectado", "", "\n", ""},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			out = ui.New(io.Discard, false)
			if got := confirmarPruebas(strings.NewReader(c.respuesta), c.detectado); got != c.quiere {
				t.Errorf("confirmarPruebas = %q, quiere %q", got, c.quiere)
			}
		})
	}
}

func TestConfirmarPruebasMuestraLoDetectado(t *testing.T) {
	var b strings.Builder
	out = ui.New(&b, false)
	confirmarPruebas(strings.NewReader("\n"), "make test")
	if !strings.Contains(b.String(), "make test") {
		t.Errorf("init debió mostrar el comando detectado, escribió:\n%s", b.String())
	}
}

func TestRunInitBanderaPruebas(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "pytest -q", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pruebas != "pytest -q" {
		t.Errorf("Pruebas = %q, quiere la bandera", cfg.Pruebas)
	}
	if len(cfg.PatronesPrueba) == 0 {
		t.Error("init debió sembrar patrones_prueba")
	}
}

func repoTemporal(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if err := cmd.Run(); err != nil {
			t.Skipf("sin git utilizable: %v", err)
		}
	}
	// git rev-parse --show-toplevel resuelve los enlaces de /tmp
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return real
}

func TestRunInitPlantillaGo(t *testing.T) {
	root := repoTemporal(t)
	var b strings.Builder
	out = ui.New(&b, false)
	if err := runInit(root, "", "go", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pruebas != "go vet ./... && go test ./..." {
		t.Errorf("Pruebas = %q, quiere plantilla go", cfg.Pruebas)
	}
	if !strings.Contains(b.String(), "go test ./internal/") {
		t.Errorf("init debió sugerir listo_cuando de go, escribió:\n%s", b.String())
	}
}

func TestRunInitPlantillaDesconocida(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	err := runInit(root, "", "elixir", "", nil, true)
	if err == nil || !strings.Contains(err.Error(), "plantilla desconocida") {
		t.Fatalf("error = %v, quiere plantilla desconocida", err)
	}
}

func TestRunInitPruebasGanaAPlantilla(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "make test", "go", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pruebas != "make test" {
		t.Errorf("Pruebas = %q, --pruebas debe ganar", cfg.Pruebas)
	}
}
