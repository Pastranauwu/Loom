package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

const diagnosticoFalso = `{"title":"rechaza id vacío","cause":"Get no valida","invariant":"Get nunca recibe un id vacío","criterion":"un id vacío se rechaza","command":"go test ./store -run TestVacio","test_file":"store/vacio_test.go","only_touch":["store/store.go"],"how":"Get(\"\") -> error"}`

func TestCurarEditaElSpecCreaLaTareaYNoDejaDeltaPendiente(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(root, "loom.spec.yml")
	if err := os.WriteFile(ruta, []byte("# mío\nfeature: tienda\nrequirements:\n  - guardar pedidos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := spec.Load(ruta)
	if err := spec.SaveEstadoSpec(root, "loom.spec.yml", spec.Snapshot(s)); err != nil {
		t.Fatal(err)
	}
	if err := spec.SaveFeatureState(root, s); err != nil {
		t.Fatal(err)
	}

	gen := &generadorGuion{respuestas: []string{diagnosticoFalso}}
	tk, err := curar(root, config.Config{Pruebas: "go test ./..."}, gen, "panic: id vacío\n\tstore.go:12", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID != "T-001" || !strings.Contains(gen.prompts[0], "panic: id vacío") || !strings.Contains(gen.prompts[0], "guardar pedidos") {
		t.Errorf("tarea %q, prompt:\n%s", tk.ID, gen.prompts[0])
	}
	cargada, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil || cargada.ListoCuando != "go test ./store -run TestVacio" {
		t.Fatalf("tarea guardada = %+v, %v", cargada, err)
	}
	b, _ := os.ReadFile(ruta)
	if !strings.Contains(string(b), "# mío") || !strings.Contains(string(b), "Get nunca recibe un id vacío") {
		t.Errorf("spec editado:\n%s", b)
	}
	nuevo, _ := spec.Load(ruta)
	prev, ok := spec.Previo(root, "loom.spec.yml", nuevo)
	if !ok || !spec.Diff(prev, spec.Snapshot(nuevo)).Vacio() {
		t.Errorf("apply vería la regla agregada como delta: %v", spec.Diff(prev, spec.Snapshot(nuevo)))
	}
	if f, _ := spec.LoadFeatureState(root); len(f.AcceptanceCommands()) != 1 {
		t.Errorf("el comando debe entrar a la aceptación global: %+v", f.Acceptance)
	}
}

func TestCurarDryRunNoTocaNada(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	os.MkdirAll(config.Dir(root), 0o755)
	ruta := filepath.Join(root, "loom.spec.yml")
	os.WriteFile(ruta, []byte("feature: t\nrequirements:\n  - a\n"), 0o644)
	gen := &generadorGuion{respuestas: []string{diagnosticoFalso}}
	if _, err := curar(root, config.Config{}, gen, "boom", "", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(ruta); string(b) != "feature: t\nrequirements:\n  - a\n" {
		t.Error("dry-run no debe editar el spec")
	}
	if es, _ := os.ReadDir(config.TasksDir(root)); len(es) != 0 {
		t.Error("dry-run no debe crear tareas")
	}
	if _, err := curar(t.TempDir(), config.Config{}, gen, "boom", "", true); err == nil {
		t.Error("sin spec debe fallar con una pista")
	}
}
