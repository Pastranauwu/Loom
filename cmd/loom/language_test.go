package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestLanguageGuardaPreferenciaYConfigDelProyecto(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOOM_LANGUAGE", "")
	defer idioma.Set("")
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "true", "", "", nil, true); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cmd := newLanguageCmd()
	cmd.SetArgs([]string{"es"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if idioma.Preferido() != "es" {
		t.Error("debe guardar el idioma del usuario")
	}
	if cfg, err := config.Load(root); err != nil || cfg.Language != "es" {
		t.Errorf("config del proyecto: %v %+v", err, cfg)
	}
	bad := newLanguageCmd()
	bad.SetArgs([]string{"fr"})
	bad.SilenceUsage, bad.SilenceErrors = true, true
	if err := bad.Execute(); err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Errorf("un idioma desconocido debe fallar: %v", err)
	}
}
