package main

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/budget"
)

func TestTopeTareaCortaSoloLaTarea(t *testing.T) {
	global := budget.Nuevo(0)
	tp := nuevoTopeTarea(t.TempDir(), "T-027", 1000, global)
	if !tp.Gastar(600) || tp.Motivo() != "" {
		t.Fatal("bajo el tope debía seguir")
	}
	if tp.Gastar(600) {
		t.Fatal("pasó el tope y siguió")
	}
	if !strings.Contains(tp.Motivo(), "tope_tokens_tarea") {
		t.Fatalf("motivo = %q", tp.Motivo())
	}
}
