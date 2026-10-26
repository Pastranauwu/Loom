package main

import (
	"reflect"
	"testing"

	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

// Lo que escribió el humano manda; el planificador solo llena huecos, y
// si numera por su cuenta, su "T-001" es la primera de la lista.
func TestCompletarTareaNoPisaAlHumano(t *testing.T) {
	ids := []string{"T-003", "T-004"}
	humano := task.Task{ID: "T-004", Titulo: "endpoint", ListoCuando: "go test ./api", LimiteLineas: 200}
	b := plan.Borrador{
		Titulo: "endpoint", ListoCuando: "otro comando", TocarSolo: []string{"api/**"},
		DependeDe: []string{"T-001"}, Peso: "media", Agente: "backend", Como: "empieza por el handler", LimiteLineas: 350,
	}

	got := completarTarea(humano, b, ids, 200, "")
	if got.ListoCuando != "go test ./api" {
		t.Errorf("pisó el listo_cuando del humano: %q", got.ListoCuando)
	}
	if !reflect.DeepEqual(got.TocarSolo, []string{"api/**"}) || !reflect.DeepEqual(got.DependeDe, []string{"T-003"}) {
		t.Errorf("tocar_solo %v · depende_de %v", got.TocarSolo, got.DependeDe)
	}
	if got.Notas != "empieza por el handler" || got.LimiteLineas != 200 || got.Agente != "backend" {
		t.Errorf("notas %q · limite %d · agente %q", got.Notas, got.LimiteLineas, got.Agente)
	}
	if conSpec := completarTarea(humano, b, ids, 200, "frontend"); conSpec.Agente != "" {
		t.Errorf("con agente por defecto en el spec, el modelo no elige: %q", conSpec.Agente)
	}
}

func TestFaltaContrato(t *testing.T) {
	solo := task.Task{Titulo: "x", ListoCuando: "true"}
	if faltaContrato(solo, 1) {
		t.Error("una tarea sola con listo_cuando ya es contrato")
	}
	if !faltaContrato(solo, 2) {
		t.Error("con hermanas, sin tocar_solo la esclusa la rechaza")
	}
	if !faltaContrato(task.Task{Titulo: "x"}, 1) {
		t.Error("sin listo_cuando falta contrato")
	}
}
