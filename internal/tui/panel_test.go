package tui

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/state"
)

func TestPanelPestanasYScroll(t *testing.T) {
	m := panelModel{ancho: 40, alto: 12, mapaTexto: strings.Split(strings.Repeat("fila\n", 30), "\n"),
		filas: []Fila{{ID: "T-001", Titulo: "uno", Estado: state.Lista}, {ID: "T-002", Titulo: "dos", Estado: state.Pendiente}}}
	if v := stripANSI(m.View()); !strings.Contains(v, "T-001") || !strings.Contains(v, "T-002") {
		t.Fatalf("la pestaña de tareas no lista las tareas:\n%s", v)
	}
	m = m.tecla("j")
	if m.cursor != 1 {
		t.Fatalf("j debe mover el cursor, cursor=%d", m.cursor)
	}
	m = m.tecla("tab")
	if m.tab != 1 || !strings.Contains(stripANSI(m.View()), "fila") {
		t.Fatal("tab debe pasar al mapa")
	}
	m = m.tecla("G")
	if v := m.View(); strings.Count(v, "\n") >= m.alto {
		t.Fatalf("G no debe pasarse del alto:\n%s", v)
	}
}
