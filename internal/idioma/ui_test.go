package idioma

import (
	"strings"
	"testing"
)

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

func verbos(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			continue
		}
		j := i + 1
		for j < len(s) && strings.ContainsRune("+-# 0123456789.", rune(s[j])) {
			j++
		}
		if j < len(s) && s[j] != '%' {
			out = append(out, string(s[j]))
		}
		i = j
	}
	return out
}

func TestTodasLasTraduccionesConservanLosVerbos(t *testing.T) {
	for es, en := range textos {
		if strings.Join(verbos(es), "") != strings.Join(verbos(en), "") {
			t.Errorf("verbos distintos:\n  es: %q\n  en: %q", es, en)
		}
		if en == "" {
			t.Errorf("traducción vacía: %q", es)
		}
	}
}

func TestIdiomaPreferidoDelUsuarioYOrden(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOOM_LANGUAGE", "")
	Set("")
	if Actual() != "en" {
		t.Error("sin nada elegido el idioma es inglés")
	}
	if err := GuardarPreferido("es"); err != nil {
		t.Fatal(err)
	}
	Set("")
	if Actual() != "es" || Preferido() != "es" {
		t.Error("el idioma del usuario aplica sin config de proyecto")
	}
	Set("en")
	if Actual() != "en" {
		t.Error("el config del proyecto gana sobre el idioma del usuario")
	}
	t.Setenv("LOOM_LANGUAGE", "es")
	Set("en")
	if Actual() != "es" {
		t.Error("LOOM_LANGUAGE gana sobre todo")
	}
	t.Setenv("LOOM_LANGUAGE", "")
	Set("en")
	if GuardarPreferido("fr") == nil {
		t.Error("un idioma desconocido no se guarda")
	}
}
