package main

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/ui"
)

func TestNotasListoCuandoConStack(t *testing.T) {
	got := notasListoCuando("go")
	if !strings.Contains(got, "hoy falla") {
		t.Errorf("notas sin regla de oro:\n%s", got)
	}
	if !strings.Contains(got, "go test ./internal/") {
		t.Errorf("notas sin ejemplo de go:\n%s", got)
	}
}

func TestNotasListoCuandoSinStack(t *testing.T) {
	got := notasListoCuando("")
	if got != reglaOroListoCuando {
		t.Errorf("notas = %q, quiere solo la regla de oro", got)
	}
	if strings.Contains(got, "go test") || strings.Contains(got, "pytest") || strings.Contains(got, "jest") {
		t.Errorf("sin stack no se inventa plantilla:\n%s", got)
	}
}

func TestImprimirPlantillasListoCuando(t *testing.T) {
	var b strings.Builder
	out = ui.New(&b, false)
	imprimirPlantillasListoCuando("python")
	if !strings.Contains(b.String(), "pytest tests/") {
		t.Errorf("debió imprimir ejemplos de python, escribió:\n%s", b.String())
	}

	b.Reset()
	out = ui.New(&b, false)
	imprimirPlantillasListoCuando("rust")
	if !strings.Contains(b.String(), reglaOroListoCuando) {
		t.Errorf("stack desconocido debió imprimir la regla de oro, escribió:\n%s", b.String())
	}
	if strings.Contains(b.String(), "cargo") {
		t.Errorf("stack desconocido inventó ejemplos:\n%s", b.String())
	}
}
