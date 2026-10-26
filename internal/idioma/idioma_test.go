package idioma

import (
	"strings"
	"testing"
)

func TestEtiquetasEnAmbosIdiomas(t *testing.T) {
	if !Tiene("Cases: a -> b", Casos) || !Tiene("Casos: a -> b", Casos) || Tiene("Examples:", Casos) {
		t.Error("Casos/Cases")
	}
	if _, resto, ok := Cortar("x Cases: 1", Casos); !ok || strings.TrimSpace(resto) != "1" {
		t.Errorf("Cortar = %q %v", resto, ok)
	}
}

func TestNormalizarClavesNoPisaLaCanonica(t *testing.T) {
	got := string(NormalizarClaves([]byte(`{"title":"a","titulo":"b","why":"c"}`), map[string]string{"title": "titulo", "why": "porque"}))
	if !strings.Contains(got, `"titulo":"b"`) || !strings.Contains(got, `"porque":"c"`) || strings.Contains(got, "title") {
		t.Errorf("got %s", got)
	}
}

func TestInstruccionSoloEnEspanol(t *testing.T) {
	Set("en")
	if Instruccion() != "" {
		t.Error("en no agrega nada")
	}
	Set("es")
	defer Set("en")
	if !strings.Contains(Instruccion(), "Spanish") {
		t.Error("es pide prosa en español")
	}
}
