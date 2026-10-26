package tui

import (
	"github.com/Pastranauwu/loom/internal/idioma"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Opcion es una fila elegible de una lista.
type Opcion struct {
	ID       string
	Etiqueta string
	Detalle  string // segunda línea, apagada; vacío para omitirla
	Marcada  bool   // estado inicial en las listas de marcar
}

// Elegir muestra una lista de selección única. Devuelve el id elegido, o
// "" si el usuario canceló con q o esc.
func Elegir(titulo, ayuda string, ops []Opcion) (string, error) {
	if len(ops) == 0 {
		return "", nil
	}
	if ayuda == "" {
		ayuda = idioma.T("j/k mueve · enter elige · q cancela")
	}
	m := listaModel{titulo: titulo, ayuda: ayuda, ops: ops, ayudaMulti: false}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	f := final.(listaModel)
	if !f.confirmado {
		return "", nil
	}
	return f.ops[f.cursor].ID, nil
}

// Marcar muestra una lista de selección múltiple. Devuelve los ids
// marcados y si el usuario confirmó (false si canceló).
func Marcar(titulo, ayuda string, ops []Opcion) ([]string, bool, error) {
	if len(ops) == 0 {
		return nil, true, nil
	}
	if ayuda == "" {
		ayuda = idioma.T("j/k mueve · espacio marca · a todas · n ninguna · enter confirma · q cancela")
	}
	m := listaModel{titulo: titulo, ayuda: ayuda, ops: ops, ayudaMulti: true}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, false, err
	}
	f := final.(listaModel)
	if !f.confirmado {
		return nil, false, nil
	}
	var ids []string
	for _, o := range f.ops {
		if o.Marcada {
			ids = append(ids, o.ID)
		}
	}
	return ids, true, nil
}

type listaModel struct {
	titulo     string
	ayuda      string
	ops        []Opcion
	cursor     int
	ayudaMulti bool
	confirmado bool
}

func (m listaModel) Init() tea.Cmd { return nil }

func (m listaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = (m.cursor + 1) % len(m.ops)
	case "k", "up":
		m.cursor = (m.cursor - 1 + len(m.ops)) % len(m.ops)
	case " ":
		if m.ayudaMulti {
			m.ops[m.cursor].Marcada = !m.ops[m.cursor].Marcada
		}
	case "a":
		if m.ayudaMulti {
			for i := range m.ops {
				m.ops[i].Marcada = true
			}
		}
	case "n":
		if m.ayudaMulti {
			for i := range m.ops {
				m.ops[i].Marcada = false
			}
		}
	case "enter":
		m.confirmado = true
		return m, tea.Quit
	}
	return m, nil
}

// maxVisibles acota cuántas opciones se pintan a la vez. Un catálogo de
// modelos trae decenas de entradas y sin ventana la lista se salía de la
// pantalla: el cursor bajaba a ciegas.
const maxVisibles = 12

func (m listaModel) View() string {
	var b strings.Builder
	b.WriteString(estiloTitulo.Render(m.titulo) + "\n\n")

	desde, hasta := ventana(m.cursor, len(m.ops), maxVisibles)
	if desde > 0 {
		b.WriteString(estiloApagado.Render("  ↑ "+strconv.Itoa(desde)+idioma.T(" más arriba")) + "\n")
	}
	for i := desde; i < hasta; i++ {
		b.WriteString(renderOpcion(m.ops[i], i == m.cursor, m.ayudaMulti) + "\n")
	}
	if hasta < len(m.ops) {
		b.WriteString(estiloApagado.Render("  ↓ "+strconv.Itoa(len(m.ops)-hasta)+idioma.T(" más abajo")) + "\n")
	}

	b.WriteString("\n" + estiloApagado.Render(m.ayuda))
	return Logo(80) + "\n" + caja(b.String())
}

// ventana devuelve el rango visible que mantiene al cursor dentro,
// centrado salvo en los extremos.
func ventana(cursor, total, max int) (desde, hasta int) {
	if total <= max {
		return 0, total
	}
	desde = cursor - max/2
	if desde < 0 {
		desde = 0
	}
	if desde+max > total {
		desde = total - max
	}
	return desde, desde + max
}

func renderOpcion(o Opcion, seleccionada, multi bool) string {
	marca := "  "
	if seleccionada {
		marca = estiloPresion.Render("> ")
	}
	casilla := ""
	if multi {
		if o.Marcada {
			casilla = estiloPresion.Render("[x] ")
		} else {
			casilla = estiloApagado.Render("[ ] ")
		}
	}
	etiqueta := o.Etiqueta
	if seleccionada {
		etiqueta = estiloBold.Render(etiqueta)
	} else {
		etiqueta = estiloTinta.Render(etiqueta)
	}
	linea := marca + casilla + etiqueta
	if o.Detalle != "" {
		linea += "\n      " + estiloApagado.Render(o.Detalle)
	}
	return linea
}
