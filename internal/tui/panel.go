package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/idioma"
)

func rgbColor(c [3]int) lipgloss.Color { return lipgloss.Color(mezclar(c, c, 0)) }

func pintar(ls []lineaSticker, ancho int) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = lipgloss.NewStyle().Foreground(rgbColor(l.color)).MaxWidth(ancho).Render(l.texto)
	}
	return out
}

type (
	panelTick struct{}
	panelMapa string
)

// Panel is the browsable pane of loom ide: task board and repo map in one
// view. tab switches, j/k scroll, enter opens a task, esc goes back.
func Panel(root string, mapa func(ancho int) string) error {
	m := panelModel{root: root, mapa: mapa}
	m.cargar()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type panelModel struct {
	root        string
	mapa        func(ancho int) string
	tab         int // 0 tareas, 1 mapa
	filas       []Fila
	cursor      int
	detalleID   string
	detalle     []string
	mapaTexto   []string
	offset      int
	ancho, alto int
}

func (m panelModel) Init() tea.Cmd { return tea.Batch(panelPulso(), m.pedirMapa()) }

func panelPulso() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return panelTick{} })
}

func (m panelModel) pedirMapa() tea.Cmd {
	ancho := max(m.ancho, 30)
	mapa := m.mapa
	return func() tea.Msg { return panelMapa(mapa(ancho)) }
}

func (m *panelModel) cargar() {
	if filas, err := Tablero(m.root); err == nil {
		m.filas = filas
		m.cursor = max(0, min(m.cursor, len(planas(filas))-1))
	}
	if m.detalleID != "" {
		m.detalle = textoPintado(detalleTarea(m.root, m.detalleID, m.ancho))
	}
}

func textoPintado(ls []lineaSticker) []string { return pintar(ls, 1000) }

// planas lists the selectable tasks in the order the board draws them.
func planas(filas []Fila) []Fila {
	var out []Fila
	for _, col := range columnas {
		for _, f := range filas {
			if f.Estado == col.estado {
				out = append(out, f)
			}
		}
	}
	return out
}

func (m panelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch k := msg.(type) {
	case tea.WindowSizeMsg:
		m.ancho, m.alto = k.Width, k.Height
		return m, m.pedirMapa()
	case panelTick:
		m.cargar()
		return m, tea.Batch(panelPulso(), m.pedirMapa())
	case panelMapa:
		m.mapaTexto = strings.Split(string(k), "\n")
	case tea.KeyMsg:
		return m.tecla(k.String()), nil
	}
	return m, nil
}

func (m panelModel) tecla(t string) panelModel {
	if t == "ctrl+c" {
		return m
	}
	scroll := m.tab == 1 || m.detalleID != ""
	mover := func(d int) {
		if scroll {
			m.offset += d
		} else {
			m.cursor += d
		}
	}
	switch t {
	case "tab", "right", "l", "left", "h":
		m.tab = 1 - m.tab
		m.detalleID, m.offset = "", 0
	case "1":
		m.tab, m.detalleID, m.offset = 0, "", 0
	case "2":
		m.tab, m.detalleID, m.offset = 1, "", 0
	case "down", "j":
		mover(1)
	case "up", "k":
		mover(-1)
	case "pgdown", " ":
		mover(max(m.alto-3, 1))
	case "pgup":
		mover(-max(m.alto-3, 1))
	case "g", "home":
		m.offset, m.cursor = 0, 0
	case "G", "end":
		m.offset, m.cursor = 1<<30, 1<<30
	case "enter":
		if m.tab == 0 && m.detalleID == "" {
			if ps := planas(m.filas); len(ps) > 0 {
				m.detalleID, m.offset = ps[m.cursor].ID, 0
				m.detalle = textoPintado(detalleTarea(m.root, m.detalleID, m.ancho))
			}
		}
	case "esc", "backspace":
		m.detalleID, m.offset = "", 0
	}
	m.cursor = max(0, min(m.cursor, len(planas(m.filas))-1))
	return m
}

func (m panelModel) View() string {
	ancho, cuerpoAlto := max(m.ancho, 20), max(m.alto-3, 1)
	pestana := func(n int, nombre string) string {
		if m.tab == n {
			return estiloTitulo.Underline(true).Render(nombre)
		}
		return estiloApagado.Render(nombre)
	}
	cab := " " + pestana(0, idioma.T("tareas")) + estiloApagado.Render("  │  ") + pestana(1, idioma.T("mapa"))

	var lineas []string
	pie := idioma.T("tab cambia · j/k · enter detalle · esc")
	switch {
	case m.detalleID != "":
		lineas = m.detalle
	case m.tab == 1:
		lineas = m.mapaTexto
		pie = idioma.T("tab cambia · j/k desplaza · g/G inicio/fin")
	default:
		lineas, m.offset = m.listaTareas(cuerpoAlto)
	}
	if m.detalleID != "" || m.tab == 1 {
		m.offset = max(0, min(m.offset, max(len(lineas)-cuerpoAlto, 0)))
	}
	fin := min(m.offset+cuerpoAlto, len(lineas))
	visibles := lineas[min(m.offset, fin):fin]
	for len(visibles) < cuerpoAlto {
		visibles = append(visibles, "")
	}
	cuerpo := lipgloss.NewStyle().MaxWidth(ancho).Render(strings.Join(visibles, "\n"))
	return cab + "\n\n" + cuerpo + "\n" + estiloApagado.MaxWidth(ancho).Render(pie)
}

// listaTareas draws the tasks grouped by state; the window follows the cursor.
func (m panelModel) listaTareas(alto int) ([]string, int) {
	if len(m.filas) == 0 {
		return []string{estiloApagado.Render(idioma.T("sin tareas · pídele a Claude un spec"))}, 0
	}
	sel := ""
	if ps := planas(m.filas); len(ps) > 0 {
		sel = ps[m.cursor].ID
	}
	var ls []lineaSticker
	lineaSel := 0
	for _, col := range columnas {
		var suyos []Fila
		for _, f := range m.filas {
			if f.Estado == col.estado {
				suyos = append(suyos, f)
			}
		}
		if len(suyos) == 0 {
			continue
		}
		ls = append(ls, lineaSticker{texto: fmt.Sprintf("%s %s · %d", col.glifo, idioma.T(col.nombre), len(suyos)), color: col.color})
		for _, f := range suyos {
			if f.ID == sel {
				lineaSel = len(ls)
			}
			ls = append(ls, filaConHijos(f, 0, sel)...)
		}
		ls = append(ls, lineaSticker{})
	}
	desde, hasta := ventana(lineaSel, len(ls), alto)
	return pintar(ls[desde:hasta], max(m.ancho, 20)), 0
}
