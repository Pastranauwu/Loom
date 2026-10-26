package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/topo"
)

type etiquetasMapa struct {
	titulo, modulos, archivos, simbolos, listas, enCurso, pendientes, detenidas string
	aceptacion, entradas, cimientos, capa, usa, dependeDe, sinDna, leyenda      string
	verde, roja, sinCorrer, scroll                                              string
	archivo, simbolo, dependencias                                              string
}

func etiquetas() etiquetasMapa {
	if idioma.Es() {
		return etiquetasMapa{"loom map", "módulos", "archivos", "símbolos", "listas", "en curso", "pendientes", "detenidas",
			"aceptación", "Puntos de entrada", "Cimientos", "Capa", "usa", "depende de", "sin ADN · loom dna add <nombre>",
			"✓ lista  ◐ en curso  ✗ detenida  · pendiente   ◆ contrato que expone   → depende de",
			"verde", "roja", "sin correr", "↑↓ desplaza · q sale", "archivo", "símbolo", "dependencias"}
	}
	return etiquetasMapa{"loom map", "modules", "files", "symbols", "ready", "running", "pending", "stopped",
		"acceptance", "Entry points", "Foundations", "Layer", "uses", "depends on", "no DNA · loom dna add <name>",
		"✓ ready  ◐ running  ✗ stopped  · pending   ◆ contract it exposes   → depends on",
		"green", "red", "not run", "↑↓ scroll · q quits", "file", "symbol", "dependencies"}
}

// Terminal is the size of the terminal, or 100x40 if it cannot be read.
func Terminal(fd uintptr) (ancho, alto int) {
	if w, h, err := term.GetSize(fd); err == nil && w >= 40 {
		return w, h
	}
	return 100, 40
}

// VistaMapa draws the topological map as layers of module cards: entry
// points on top, foundations at the bottom, with the DNA and the gates in
// a header. It is plain text with colors; ancho is the terminal width.
func VistaMapa(m topo.Mapa, ancho int) string {
	t := etiquetas()
	var b strings.Builder
	b.WriteString(cabeceraMapa(m, t, ancho) + "\n\n")

	anchoCard := 34
	if ancho < anchoCard+4 {
		anchoCard = ancho - 4
	}
	porFila := (ancho - 2) / (anchoCard + 1)
	if porFila < 1 {
		porFila = 1
	}
	for capa := 0; capa < m.NumCapas; capa++ {
		var tarjetas []string
		for _, mod := range m.Modulos {
			if mod.Capa == capa {
				tarjetas = append(tarjetas, tarjeta(mod, anchoCard, t))
			}
		}
		if len(tarjetas) == 0 {
			continue
		}
		b.WriteString(bandaCapa(capa, m.NumCapas, ancho, t) + "\n")
		for i := 0; i < len(tarjetas); i += porFila {
			fin := min(i+porFila, len(tarjetas))
			fila := tarjetas[i:fin]
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, intercalar(fila)...) + "\n")
		}
		if capa < m.NumCapas-1 {
			b.WriteString(estiloApagado.Render("  │\n  ▼ "+t.dependeDe) + "\n")
		}
	}
	b.WriteString("\n" + estiloApagado.Width(ancho).Render(t.leyenda))
	return b.String()
}

func intercalar(ts []string) []string {
	out := make([]string, 0, len(ts)*2)
	for i, c := range ts {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, c)
	}
	return out
}

func bandaCapa(capa, total, ancho int, t etiquetasMapa) string {
	nombre := fmt.Sprintf("%s %d", t.capa, capa+1)
	switch {
	case total > 1 && capa == 0:
		nombre = t.entradas
	case total > 1 && capa == total-1:
		nombre = t.cimientos
	}
	linea := "── " + nombre + " "
	if r := ancho - 1 - len([]rune(linea)); r > 0 {
		linea += strings.Repeat("─", r)
	}
	return estiloApagado.Render(linea)
}

func chip(s string, fondo lipgloss.Color) string {
	return lipgloss.NewStyle().Background(fondo).Foreground(lipgloss.Color("#0B0B0B")).Padding(0, 1).Render(s)
}

func cabeceraMapa(m topo.Mapa, t etiquetasMapa, ancho int) string {
	var b strings.Builder
	b.WriteString(estiloTitulo.Render(t.titulo) + "  " + estiloApagado.Render(fmt.Sprintf("%d %s · %d %s", len(m.Modulos), t.modulos, len(m.Aristas), t.dependencias)) + "\n")
	var fila []string
	for _, d := range m.DNA {
		fila = append(fila, chip("◈ "+d.Nombre, presion))
	}
	if len(fila) == 0 {
		fila = append(fila, estiloApagado.Render(t.sinDna))
	}
	b.WriteString(lipgloss.NewStyle().Width(ancho).Render(strings.Join(fila, " ")) + "\n")
	c := m.Compuertas
	var estados []string
	for _, e := range []struct {
		k, nombre string
		c         lipgloss.Color
	}{{"lista", t.listas, presion}, {"en_curso", t.enCurso, espera}, {"pendiente", t.pendientes, apagado}, {"detenida", t.detenidas, alerta}} {
		if n := c.Tareas[e.k]; n > 0 {
			estados = append(estados, chip(fmt.Sprintf("%s %d %s", glifoEstado(e.k), n, e.nombre), e.c))
		}
	}
	acept, color := t.sinCorrer, apagado
	switch c.Aceptacion {
	case "verde":
		acept, color = t.verde, presion
	case "roja":
		acept, color = t.roja, alerta
	}
	estados = append(estados, chip(t.aceptacion+" "+acept, color))
	b.WriteString(lipgloss.NewStyle().Width(ancho).Render(strings.Join(estados, " ")))
	return b.String()
}

func glifoEstado(e string) string {
	switch e {
	case "lista":
		return "✓"
	case "en_curso":
		return "◐"
	case "detenida":
		return "✗"
	}
	return "·"
}

func colorModulo(mod topo.Modulo) lipgloss.Color {
	hay := map[string]bool{}
	for _, t := range mod.Tareas {
		hay[t.Estado] = true
	}
	switch {
	case hay["detenida"]:
		return alerta
	case hay["en_curso"]:
		return espera
	case len(mod.Tareas) > 0 && hay["lista"] && !hay["pendiente"]:
		return presion
	case len(mod.Tareas) > 0:
		return tinta
	}
	return apagado
}

func recorte(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// tarjeta is one module: its name, size, tasks, the contracts it exposes
// and what it depends on, in a rounded box colored by the state of its tasks.
func tarjeta(mod topo.Modulo, ancho int, t etiquetasMapa) string {
	in := ancho - 4 // border and padding
	var l []string
	l = append(l, estiloBold.Render(recorte(mod.Ruta, in)))
	l = append(l, estiloApagado.Render(fmt.Sprintf("%s · %s", cuenta(mod.Archivos, t.archivo, t.archivos), cuenta(mod.NumSimbolos, t.simbolo, t.simbolos))))
	for i, tk := range mod.Tareas {
		if i == 3 {
			l = append(l, estiloApagado.Render(fmt.Sprintf("  +%d", len(mod.Tareas)-3)))
			break
		}
		l = append(l, glifoColor(tk.Estado)+" "+recorte(tk.ID+" "+tk.Titulo, in-2))
	}
	for i, c := range mod.Expone {
		if i == 3 {
			l = append(l, estiloApagado.Render(fmt.Sprintf("  +%d", len(mod.Expone)-3)))
			break
		}
		linea := "◆ " + recorte(c.Firma, in-2)
		if len(c.Consumidores) > 0 {
			linea += estiloApagado.Render(" ← " + strings.Join(c.Consumidores, ","))
		}
		l = append(l, linea)
	}
	if len(mod.Expone) == 0 && len(mod.Simbolos) > 0 {
		l = append(l, estiloApagado.Render("◇ "+recorte(mod.Simbolos[0], in-2)))
	}
	if len(mod.Importa)+len(mod.Consume) > 0 {
		dep := unicoOrden(append(append([]string{}, mod.Importa...), mod.Consume...))
		corto := make([]string, len(dep))
		for i, d := range dep {
			corto[i] = d[strings.LastIndex(d, "/")+1:]
		}
		l = append(l, estiloApagado.Render("→ "+recorte(strings.Join(corto, ", "), in-2)))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorModulo(mod)).Padding(0, 1).Width(ancho - 2).Render(strings.Join(l, "\n"))
}

func glifoColor(e string) string {
	g := glifoEstado(e)
	switch e {
	case "lista":
		return estiloPresion.Render(g)
	case "en_curso":
		return estiloEspera.Render(g)
	case "detenida":
		return estiloAlerta.Render(g)
	}
	return estiloApagado.Render(g)
}

func unicoOrden(s []string) []string {
	visto := map[string]bool{}
	var out []string
	for _, x := range s {
		if !visto[x] {
			visto[x] = true
			out = append(out, x)
		}
	}
	return out
}

// Visor shows a long text in a scrollable full-screen view; q or esc closes it.
func Visor(contenido string) error {
	_, err := tea.NewProgram(visorModel{lineas: strings.Split(contenido, "\n")}, tea.WithAltScreen()).Run()
	return err
}

type visorModel struct {
	lineas       []string
	alto, offset int
}

func (m visorModel) Init() tea.Cmd { return nil }

func (m visorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch k := msg.(type) {
	case tea.WindowSizeMsg:
		m.alto = k.Height
	case tea.KeyMsg:
		pagina := max(m.alto-2, 1)
		switch k.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "down", "j":
			m.offset++
		case "up", "k":
			m.offset--
		case "pgdown", " ":
			m.offset += pagina
		case "pgup":
			m.offset -= pagina
		case "g", "home":
			m.offset = 0
		case "G", "end":
			m.offset = len(m.lineas)
		}
		m.offset = max(0, min(m.offset, max(len(m.lineas)-pagina, 0)))
	}
	return m, nil
}

func (m visorModel) View() string {
	pagina := max(m.alto-1, 1)
	fin := min(m.offset+pagina, len(m.lineas))
	return strings.Join(m.lineas[m.offset:fin], "\n") + "\n" + estiloApagado.Render(etiquetas().scroll)
}

// cuenta writes "1 file" or "3 files".
func cuenta(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}
