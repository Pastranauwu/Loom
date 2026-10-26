package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// PaginaTutorial is one screen of the guided walkthrough.
type PaginaTutorial struct{ Titulo, Cuerpo string }

// Tutorial walks through the pages one screen at a time.
func Tutorial(paginas []PaginaTutorial) error {
	if len(paginas) == 0 {
		return nil
	}
	_, err := tea.NewProgram(tutorialModel{paginas: paginas}, tea.WithAltScreen()).Run()
	return err
}

type tutorialModel struct {
	paginas []PaginaTutorial
	i       int
}

func (m tutorialModel) Init() tea.Cmd { return nil }

func (m tutorialModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "right", "l", "enter", " ", "n", "pgdown":
		if m.i == len(m.paginas)-1 {
			return m, tea.Quit
		}
		m.i++
	case "left", "h", "backspace", "p", "pgup":
		m.i = max(m.i-1, 0)
	case "home", "g":
		m.i = 0
	case "end", "G":
		m.i = len(m.paginas) - 1
	}
	return m, nil
}

func (m tutorialModel) View() string {
	p := m.paginas[m.i]
	var progreso strings.Builder
	for j := range m.paginas {
		if j == m.i {
			progreso.WriteString(estiloPresion.Render("●"))
		} else {
			progreso.WriteString(estiloApagado.Render("·"))
		}
		progreso.WriteString(" ")
	}
	ayuda := idioma.T("← → navegar · enter siguiente · q salir")
	if m.i == len(m.paginas)-1 {
		ayuda = idioma.T("← atrás · enter o q para salir")
	}
	cabecera := estiloTitulo.Render(fmt.Sprintf("loom · %s %d/%d", idioma.T("tutorial"), m.i+1, len(m.paginas)))
	cuerpo := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(presion).Padding(1, 2).
		Render(estiloTitulo.Render(p.Titulo) + "\n\n" + p.Cuerpo)
	return "\n " + cabecera + "\n\n" + cuerpo + "\n\n " + progreso.String() + "\n " + estiloApagado.Render(ayuda) + "\n"
}
