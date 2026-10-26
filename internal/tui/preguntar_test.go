package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func escribir(m preguntaModel, s string) preguntaModel {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(preguntaModel)
	}
	return m
}

func TestPreguntaEscribeBorraYEnvia(t *testing.T) {
	m := escribir(preguntaModel{}, "hola!")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(preguntaModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(preguntaModel)
	m = escribir(m, "ñ")
	if m.valor != "hola ñ" {
		t.Fatalf("valor = %q", m.valor)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(preguntaModel).hecho || cmd == nil {
		t.Error("enter debe terminar")
	}
	next, _ = preguntaModel{}.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !next.(preguntaModel).cancelado {
		t.Error("esc debe cancelar")
	}
}
