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

func TestRunTaskAddSugiereGo(t *testing.T) {
	root := repoTemporal(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	var b strings.Builder
	out = ui.New(&b, false)
	if err := runTaskAdd(root, "exportar csv", ""); err != nil {
		t.Fatalf("runTaskAdd: %v", err)
	}
	if !strings.Contains(b.String(), "go test ./internal/") {
		t.Errorf("task add debió sugerir plantilla go, escribió:\n%s", b.String())
	}

	got, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ListoCuando != "" {
		t.Errorf("listo_cuando = %q, debe quedar vacío", got.ListoCuando)
	}
	if !strings.Contains(got.Notas, "hoy falla") || !strings.Contains(got.Notas, "go test ./internal/") {
		t.Errorf("notas sin regla de oro ni ejemplos:\n%s", got.Notas)
	}
}

func TestRunTaskAddSinStackNoInventa(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	var b strings.Builder
	out = ui.New(&b, false)
	if err := runTaskAdd(root, "hacer algo", ""); err != nil {
		t.Fatalf("runTaskAdd: %v", err)
	}
	texto := b.String()
	for _, invento := range []string{"go test", "pytest", "jest", "npm test"} {
		if strings.Contains(texto, invento) {
			t.Errorf("sin stack inventó %q:\n%s", invento, texto)
		}
	}
	if !strings.Contains(texto, reglaOroListoCuando) {
		t.Errorf("sin stack debió imprimir la regla de oro, escribió:\n%s", texto)
	}

	got, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ListoCuando != "" {
		t.Errorf("listo_cuando = %q, debe quedar vacío", got.ListoCuando)
	}
	if got.Notas != reglaOroListoCuando {
		t.Errorf("notas = %q, quiere solo la regla de oro", got.Notas)
	}
}

func TestRunTaskAddConAgente(t *testing.T) {
	root := repoTemporal(t)
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if err := runTaskAdd(root, "diseñar base de datos", "architect"); err != nil {
		t.Fatalf("runTaskAdd con agente: %v", err)
	}

	got, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Agente != "architect" {
		t.Errorf("Agente = %q, quiero architect", got.Agente)
	}
}
