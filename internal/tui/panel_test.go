package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	abajo := m.offset
	m = m.tecla("j").tecla("j").tecla("k")
	if m.offset != abajo-1 {
		t.Fatalf("pasado el final, k sube de inmediato: offset %d, quiere %d", m.offset, abajo-1)
	}
	if m = m.tecla("rueda-arriba"); m.offset != abajo-4 {
		t.Fatalf("la rueda sube 3: offset %d", m.offset)
	}

	// Update con MouseMsg rueda arriba y abajo
	upd, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if upd.(panelModel).offset != m.offset+3 {
		t.Fatalf("Update con rueda abajo debe bajar 3: offset %d", upd.(panelModel).offset)
	}
	upd, _ = upd.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if upd.(panelModel).offset != m.offset {
		t.Fatalf("Update con rueda arriba debe devolver 3: offset %d", upd.(panelModel).offset)
	}

	// Clic en pestaña tareas (X < 15, Y = 0)
	upd, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: 0})
	if upd.(panelModel).tab != 0 {
		t.Fatalf("clic en tareas debe cambiar a tab 0, tab=%d", upd.(panelModel).tab)
	}
}
