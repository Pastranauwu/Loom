package main

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/state"
)

func TestErrorShipNoListaDetenida(t *testing.T) {
	err := errorShipNoLista("T-001", state.State{
		ID:       "T-001",
		Estado:   state.Detenida,
		Pregunta: "agotó 3 intentos · falla: unicode NFD en el comparador",
	})
	if err == nil {
		t.Fatal("debió fallar")
	}
	got := err.Error()
	for _, want := range []string{
		"T-001 está detenida",
		"unicode NFD en el comparador",
		"limite_intentos",
		"loom logs T-001",
		"edita la tarea",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error sin %q:\n%s", want, got)
		}
	}
}

func TestErrorShipNoListaDetenidaUsaUltimoError(t *testing.T) {
	err := errorShipNoLista("T-002", state.State{
		Estado:      state.Detenida,
		UltimoError: "go test: build failed",
	})
	if err == nil || !strings.Contains(err.Error(), "go test: build failed") {
		t.Fatalf("debió usar UltimoError, got %v", err)
	}
	if strings.Contains(err.Error(), "corre loom run primero") {
		t.Errorf("detenida no debe pedir run primero:\n%s", err)
	}
}

func TestErrorShipNoListaPendienteYEnCurso(t *testing.T) {
	pend := errorShipNoLista("T-003", state.State{Estado: state.Pendiente})
	if pend == nil || !strings.Contains(pend.Error(), "pendiente") || !strings.Contains(pend.Error(), "loom run primero") {
		t.Errorf("pendiente = %v", pend)
	}
	curso := errorShipNoLista("T-004", state.State{Estado: state.EnCurso})
	if curso == nil || !strings.Contains(curso.Error(), "en curso") {
		t.Errorf("en_curso = %v", curso)
	}
	if strings.Contains(curso.Error(), "loom run primero") {
		t.Errorf("en curso no debe pedir run:\n%s", curso)
	}
}
