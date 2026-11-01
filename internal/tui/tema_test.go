package tui

import (
	"strings"
	"testing"
)

func TestTemaPropio(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tm, err := NuevoTema("dracula", "nord", map[string]string{"accent": "#bd93f9"})
	if err != nil || tm.Acento != "#bd93f9" || tm.Fondo != "#2E3440" {
		t.Fatalf("tema = %+v, %v", tm, err)
	}
	if err := GuardarTemaPropio(tm); err != nil {
		t.Fatal(err)
	}
	if got, ok := BuscarTema("dracula"); !ok || got.Acento != "#bd93f9" {
		t.Fatal("el tema propio no se carga")
	}
	if _, err := NuevoTema("x", "loom", map[string]string{"accent": "morado"}); err == nil {
		t.Fatal("un color que no es #RRGGBB pasó")
	}
	if err := GuardarTemaPropio(Tema{Nombre: "nord"}); err == nil {
		t.Fatal("pisó un tema de fábrica")
	}
	CargarTemasPropios()
}

func TestBarraIDE(t *testing.T) {
	b := ansi.ReplaceAllString(BarraIDE(60, "ayuda", "ocultar"), "")
	if !strings.HasPrefix(b, " loom") || !strings.HasSuffix(b, "F12  ocultar ") || len([]rune(b)) != 60 {
		t.Fatalf("barra: %q", b)
	}
}
