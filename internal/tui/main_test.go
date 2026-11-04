package tui

import (
	"os"
	"testing"
)

// los tests fijan los textos de la interfaz en español; el inglés se prueba aparte
func TestMain(m *testing.M) {
	os.Setenv("LOOM_LANGUAGE", "es")
	os.Setenv("LOOM_ICONS", "plain") // los glifos esperados son los Unicode; los Nerd se prueban aparte
	os.Exit(m.Run())
}
