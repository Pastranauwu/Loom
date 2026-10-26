package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

const specPrueba = `version: 1
feature: "Exportación CSV"
agente: backend

tasks:
  - id: T-001
    titulo: "generador csv"
    listo_cuando: "go test ./internal/export/ -run TestCSV"
    tocar_solo: ["internal/export/**"]
    agente: backend

  - id: T-002
    titulo: "endpoint export"
    listo_cuando: "go test ./internal/api/ -run TestExport"
    tocar_solo: ["internal/api/**"]
    depende_de: ["T-001"]
`

func TestRunApply(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	specFile := filepath.Join(root, "loom.spec.yml")
	if err := os.WriteFile(specFile, []byte(specPrueba), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Dry run
	var b strings.Builder
	out = ui.New(&b, false)
	if _, err := runApply(root, "", false, true); err != nil {
		t.Fatalf("runApply dry run: %v", err)
	}
	if !strings.Contains(b.String(), "modo --dry-run") || !strings.Contains(b.String(), "T-001") {
		t.Errorf("salida dry run inesperada:\n%s", b.String())
	}

	// Tareas no deben existir aún en disco
	if _, err := task.Load(config.TasksDir(root), "T-001"); err == nil {
		t.Fatal("T-001 no debió crearse en dry run")
	}

	// 2. Apply real
	b.Reset()
	if _, err := runApply(root, "", false, false); err != nil {
		t.Fatalf("runApply real: %v", err)
	}
	if !strings.Contains(b.String(), "2 tareas aplicadas") {
		t.Errorf("salida apply inesperada:\n%s", b.String())
	}

	t1, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil {
		t.Fatalf("task.Load T-001: %v", err)
	}
	if t1.Titulo != "generador csv" || t1.Agente != "backend" {
		t.Errorf("T-001 = %+v", t1)
	}

	t2, err := task.Load(config.TasksDir(root), "T-002")
	if err != nil {
		t.Fatalf("task.Load T-002: %v", err)
	}
	if t2.Titulo != "endpoint export" || len(t2.DependeDe) != 1 || t2.DependeDe[0] != "T-001" {
		t.Errorf("T-002 = %+v", t2)
	}
}

func TestRunPs(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	var b strings.Builder
	out = ui.New(&b, false)
	// Sin tareas
	if err := runPs(root); err != nil {
		t.Fatalf("runPs sin tareas: %v", err)
	}
	if !strings.Contains(b.String(), "sin tareas") {
		t.Errorf("esperaba 'sin tareas', obtuve:\n%s", b.String())
	}

	// Con tarea
	if err := task.Save(config.TasksDir(root), task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         "probar ps",
		ListoCuando:    "true",
		Agente:         "backend",
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}); err != nil {
		t.Fatal(err)
	}

	b.Reset()
	if err := runPs(root); err != nil {
		t.Fatalf("runPs con tarea: %v", err)
	}
	if !strings.Contains(b.String(), "T-001") || !strings.Contains(b.String(), "pendiente") || !strings.Contains(b.String(), "[backend]") {
		t.Errorf("salida runPs inesperada:\n%s", b.String())
	}
}
