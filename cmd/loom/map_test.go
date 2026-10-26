package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/topo"
	"github.com/Pastranauwu/loom/internal/ui"
	"os"
)

func repoDeMapa(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(config.TasksDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Base: "main", DNA: []string{"zero-deps"}}).Save(root); err != nil {
		t.Fatal(err)
	}
	tareas := []task.Task{
		{Version: task.Version, ID: "T-001", Titulo: "dominio", ListoCuando: "true", TocarSolo: []string{"internal/domain/**"}, Expone: []string{"domain.Nueva() Orden"}, LimiteIntentos: 3},
		{Version: task.Version, ID: "T-002", Titulo: "http", ListoCuando: "true", TocarSolo: []string{"internal/adapters/**"}, Usa: []string{"domain.Nueva() Orden"}, DependeDe: []string{"T-001"}, LimiteIntentos: 3},
	}
	for _, tk := range tareas {
		if err := task.Save(config.TasksDir(root), tk); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.Save(root, state.State{ID: "T-001", Estado: state.Lista}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMapEnTresFormatos(t *testing.T) {
	root := repoDeMapa(t) // sin git: el mapa sale de los contratos

	var b strings.Builder
	out = ui.New(&b, false)
	if err := runMap(root, false, false, 2); err != nil {
		t.Fatal(err)
	}
	for _, quiero := range []string{"zero-deps", "✓ T-001", "internal/domain", "expone  domain.Nueva() Orden", "← T-002", "consume → internal/domain"} {
		if !strings.Contains(b.String(), quiero) {
			t.Errorf("el árbol no contiene %q:\n%s", quiero, b.String())
		}
	}

	b.Reset()
	if err := runMap(root, true, false, 2); err != nil || !strings.Contains(b.String(), "flowchart LR") || !strings.Contains(b.String(), `-. "Nueva" .->`) {
		t.Errorf("mermaid: %v\n%s", err, b.String())
	}

	b.Reset()
	out = ui.New(&b, true)
	if err := runMap(root, false, false, 2); err != nil {
		t.Fatal(err)
	}
	var m topo.Mapa
	if err := json.Unmarshal([]byte(b.String()), &m); err != nil || len(m.Modulos) != 2 || m.Compuertas.Tareas["lista"] != 1 || !m.Compuertas.DNA {
		t.Errorf("json: %v\n%s", err, b.String())
	}
}
