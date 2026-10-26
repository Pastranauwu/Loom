package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Preguntar muestra un campo de texto de una línea. Devuelve lo escrito
// (o defecto si solo se pulsa enter) y false si el usuario canceló con esc
// o ctrl+c.
func Preguntar(titulo, ayuda, defecto string) (string, bool, error) {
	if ayuda == "" {
		ayuda = "enter envía · esc cancela"
	}
	final, err := tea.NewProgram(preguntaModel{titulo: titulo, ayuda: ayuda}).Run()
	if err != nil {
		return "", false, err
	}
	f := final.(preguntaModel)
	if f.cancelado {
		return "", false, nil
	}
	if strings.TrimSpace(f.valor) == "" {
		return defecto, true, nil
	}
	return strings.TrimSpace(f.valor), true, nil
}

type preguntaModel struct {
	titulo, ayuda, valor string
	hecho, cancelado     bool
}

func (m preguntaModel) Init() tea.Cmd { return nil }

func (m preguntaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.Type {
	case tea.KeyEnter:
		m.hecho = true
		return m, tea.Quit
	case tea.KeyEsc, tea.KeyCtrlC:
		m.cancelado = true
		return m, tea.Quit
	case tea.KeyBackspace:
		if r := []rune(m.valor); len(r) > 0 {
			m.valor = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.valor += " "
	case tea.KeyRunes:
		m.valor += string(k.Runes)
	}
	return m, nil
}

func (m preguntaModel) View() string {
	if m.hecho || m.cancelado {
		return ""
	}
	return Titulo(m.titulo) + "\n" + Apagado(m.ayuda) + "\n" + Presion("› ") + m.valor + Presion("▏") + "\n"
}
