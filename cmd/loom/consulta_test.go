package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
)

func TestConsultaContinuacionYPendiente(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(loop.RunsDir(root), "T-001", "consulta.json")
	c := consulta{Pedido: "h", Sesion: "s", Resumen: "un panel", Preguntas: []string{"¿borrar o archivar? (archivar)", "¿CSV? (no)"}}
	if err := guardarConsulta(p, c); err != nil {
		t.Fatal(err)
	}
	got, pend, err := consultaPendiente(root)
	if err != nil || got != p || len(pend.Preguntas) != 2 {
		t.Fatalf("pendiente = %q %v %v", got, pend, err)
	}
	pend.Respuestas = []string{"borrar", ""}
	if err := guardarConsulta(p, pend); err != nil {
		t.Fatal(err)
	}
	if _, _, err := consultaPendiente(root); err == nil {
		t.Error("ya contestada: no queda pendiente")
	}
	txt := pend.continuacion()
	if !strings.Contains(txt, "→ borrar") || !strings.Contains(txt, "→ (use your default)") || !strings.Contains(txt, "complete JSON") {
		t.Errorf("continuacion = %q", txt)
	}
	if s := (consulta{}).continuacion(); strings.Contains(s, "Answers") {
		t.Errorf("sin preguntas no hay bloque de respuestas: %q", s)
	}
}
