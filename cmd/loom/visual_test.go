package main

import (
	"os"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func TestParseVisual(t *testing.T) {
	cumple, cambios, ok := parseVisual("Miré las capturas.\n```json\n{\"cumple\": false, \"cambios\": [\"el fondo es plano\"]}\n```")
	if !ok || cumple || len(cambios) != 1 {
		t.Fatalf("%v %v %v", cumple, cambios, ok)
	}
	if _, _, ok := parseVisual("no pude abrir las imágenes"); ok {
		t.Error("sin JSON no hay veredicto")
	}
}

func TestVeImagenes(t *testing.T) {
	for modelo, quiero := range map[string]bool{
		"claude-sonnet-5-5":             true,
		"gpt-5":                         true,
		"deepseek/deepseek-flash":       false,
		"openrouter/anthropic/claude-x": true,
		"google/gemini-3-pro":           true,
		"opencode/qwen3-coder-free":     false,
	} {
		if got := veImagenes(modelo, "claude"); got != quiero {
			t.Errorf("veImagenes(%q) = %v, quiero %v", modelo, got, quiero)
		}
	}
	if veImagenes("", "opencode") {
		t.Error("opencode sin modelo no garantiza visión")
	}
}

func TestUltimaDeUISoloRevisaElFinalDeLaCadena(t *testing.T) {
	root := t.TempDir()
	dir := config.TasksDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	diseno := task.Task{ID: "T-001", Titulo: "diseño", ListoCuando: "go test ./...", TocarSolo: []string{"static/base.css"}}
	pantalla := task.Task{ID: "T-002", Titulo: "pantalla", ListoCuando: "go test ./...", TocarSolo: []string{"templates/a.html"}, DependeDe: []string{"T-001"}}
	api := task.Task{ID: "T-003", Titulo: "api", ListoCuando: "go test ./...", TocarSolo: []string{"api/x.go"}, DependeDe: []string{"T-002"}}
	for _, x := range []task.Task{diseno, pantalla, api} {
		if err := task.Save(dir, x); err != nil {
			t.Fatal(err)
		}
	}
	if ultimaDeUI(root, diseno) {
		t.Error("el diseño tiene una pantalla pendiente detrás: no se revisa todavía")
	}
	if !ultimaDeUI(root, pantalla) {
		t.Error("la pantalla es la última de interfaz (la api no dibuja)")
	}
	_ = state.Save(root, state.State{ID: "T-002", Estado: state.Lista})
	if !ultimaDeUI(root, diseno) {
		t.Error("con la pantalla lista, rehacer el diseño sí se revisa")
	}
}
