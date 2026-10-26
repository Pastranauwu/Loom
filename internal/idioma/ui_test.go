package idioma

import "testing"

func TestTTraduceSoloEnIngles(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "")
	Set("en")
	if T("LISTO PARA ENTREGAR") != "READY TO SHIP" || T("texto sin traducir") != "texto sin traducir" {
		t.Error("en: traduce lo conocido y deja el resto")
	}
	Set("es")
	defer Set("en")
	if T("LISTO PARA ENTREGAR") != "LISTO PARA ENTREGAR" {
		t.Error("es: no cambia nada")
	}
}

func TestVariableDeEntornoManda(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "es")
	Set("en")
	if !Es() {
		t.Error("LOOM_LANGUAGE=es debe ganar sobre config")
	}
}
