package tui

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
)

// FilaRun es una tarea que va a correr, mostrada en el tablero en vivo.
type FilaRun struct {
	ID     string
	Titulo string
	Limite int
}

// EventoRun es un cambio de estado de una tarea en curso.
type EventoRun struct {
	ID       string
	Estado   string // trabajando | lista | detenida
	Intentos int
	Motivo   string
}

type tareaViva struct {
	estado   string
	intentos int
	motivo   string
}

// FaseRun es el detalle en vivo de una tarea que trabaja: en qué fase
// está, desde cuándo y cuánto lleva gastado. Sale del latido que escribe
// internal/loop, lo único que sabe qué pasa DENTRO de un intento.
type FaseRun struct {
	Intento   int
	Limite    int
	Fase      string
	Modelo    string
	DesdeFase time.Time
	Entrada   int
	Salida    int
	Atascada  bool
}

// ticksPorLatido: tickCmd late cada 80 ms, así que doce ticks es ~1 s.
const ticksPorLatido = 12

type runMsg struct {
	evento *EventoRun
	fin    bool
}

type runModel struct {
	filas  []FilaRun
	ch     chan runMsg
	estado map[string]*tareaViva
	inicio map[string]time.Time
	fases  func() map[string]FaseRun // lee los latidos de disco
	fase   map[string]FaseRun
	tick   int
	fin    bool
	alto   int
}

// CorrerRun muestra la corrida en vivo. `fases` se consulta en cada tick
// para saber qué está haciendo cada tarea ahora mismo: sin eso el tablero
// solo sabía "trabajando" y un reloj, y una invocación de veinte minutos
// se veía igual que una colgada.
func CorrerRun(filas []FilaRun, fases func() map[string]FaseRun, lanzar func(emit func(EventoRun))) error {
	ch := make(chan runMsg)
	go func() {
		lanzar(func(e EventoRun) { ch <- runMsg{evento: &e} })
		ch <- runMsg{fin: true}
		close(ch)
	}()

	if fases == nil {
		fases = func() map[string]FaseRun { return nil }
	}
	m := runModel{
		filas:  filas,
		ch:     ch,
		estado: map[string]*tareaViva{},
		inicio: map[string]time.Time{},
		fases:  fases,
		fase:   fases(),
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func (m runModel) Init() tea.Cmd {
	return tea.Batch(m.escuchar(), tickCmd())
}

func (m runModel) escuchar() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.ch
		if !ok {
			return tea.Quit()
		}
		return msg
	}
}

func (m runModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.fin {
			return m, nil
		}
		m.tick++
		// el spinner necesita 80 ms; los latidos no. Releerlos a ese
		// ritmo son doce lecturas de disco por segundo y por tarea para
		// pintar un reloj que avanza en segundos.
		if m.tick%ticksPorLatido == 0 {
			m.fase = m.fases()
		}
		return m, tickCmd()
	case tea.WindowSizeMsg:
		m.alto = msg.Height
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case runMsg:
		if msg.fin {
			m.fin = true
			return m, tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
		}
		e := msg.evento
		v := m.estado[e.ID]
		if v == nil {
			v = &tareaViva{}
			m.estado[e.ID] = v
		}
		if e.Estado == "trabajando" {
			m.inicio[e.ID] = time.Now()
		}
		v.estado = e.Estado
		v.intentos = e.Intentos
		v.motivo = e.Motivo
		return m, m.escuchar()
	}
	return m, nil
}

func (m runModel) View() string {
	var cuerpo strings.Builder

	hechos := 0
	for _, f := range m.filas {
		if v := m.estado[f.ID]; v != nil && v.estado != "trabajando" {
			hechos++
		}
	}
	total := len(m.filas)
	if total == 0 {
		total = 1
	}
	cuerpo.WriteString(estiloTitulo.Render(idioma.T("CORRIDA")) + "\n")
	cuerpo.WriteString(barra(hechos, total, 40) + " " +
		estiloApagado.Render(strconv.Itoa(hechos)+"/"+strconv.Itoa(len(m.filas))) + "\n\n")

	cabecera := cuerpo.String()
	// logo (6) + borde y relleno de la caja (4) + cabecera (3)
	const marco = 13
	filas := m.renderFilas(false)
	if m.alto > 0 && marco+strings.Count(filas, "\n") > m.alto {
		// no cabe: sin logo y solo lo que está pasando. bubbletea corta
		// por arriba lo que sobra, y con muchas tareas quedaban a la
		// vista las pendientes del final y no las que trabajan
		return caja(cabecera + strings.TrimRight(m.renderFilas(true), "\n"))
	}
	return Logo(80) + "\n" + caja(cabecera+strings.TrimRight(filas, "\n"))
}

// renderFilas pinta una línea por tarea. En compacto, las verdes y las
// pendientes se resumen en una cuenta y quedan las que trabajan o se
// detuvieron, que son las que hay que mirar.
func (m runModel) renderFilas(compacto bool) string {
	var b strings.Builder
	verdes, pendientes := 0, 0
	for _, f := range m.filas {
		v := m.estado[f.ID]
		if compacto && v == nil {
			pendientes++
			continue
		}
		if compacto && v.estado == "lista" {
			verdes++
			continue
		}
		b.WriteString(renderFilaRun(f, v, m.inicio[f.ID], m.tick))
		if fase, hay := m.fase[f.ID]; hay && (v == nil || v.estado == "trabajando") {
			b.WriteString(renderFase(fase))
		}
	}
	if compacto {
		b.WriteString("  " + estiloPresion.Render("✓ "+strconv.Itoa(verdes)+" "+idioma.T("verdes")) + estiloApagado.Render(" · "+strconv.Itoa(pendientes)+" "+idioma.T("pendientes")) + "\n")
	}
	return b.String()
}

// renderFase pinta la línea de detalle: intento, fase, modelo, tiempo en
// esa fase y tokens gastados. Es lo que responde "¿sigue viva?".
func renderFase(f FaseRun) string {
	txt := fmt.Sprintf(idioma.T("intento %d"), f.Intento)
	if f.Limite > 0 {
		txt += fmt.Sprintf("/%d", f.Limite)
	}
	txt += " · " + idioma.T(f.Fase)
	if f.Modelo != "" {
		txt += " · " + f.Modelo
	}
	txt += " · " + reloj(time.Since(f.DesdeFase))
	if f.Entrada+f.Salida > 0 {
		txt += fmt.Sprintf(" · %d↑/%d↓ tokens", f.Entrada, f.Salida)
	}
	if f.Atascada {
		return "      " + estiloAlerta.Render(idioma.T("ATASCO · ")+txt+idioma.T(" sin señal")) + "\n"
	}
	return "      " + estiloApagado.Render(txt) + "\n"
}

func renderFilaRun(f FilaRun, v *tareaViva, inicio time.Time, tick int) string {
	var g, estado string
	switch {
	case v == nil:
		g = estiloApagado.Render("·")
		estado = estiloApagado.Render(idioma.T("pendiente"))
	case v.estado == "lista":
		g = estiloPresion.Render("✓")
		estado = estiloPresion.Render(idioma.T("verde en ") + intentosTxt(v.intentos))
	case v.estado == "sin_saldo":
		g = estiloAlerta.Render("⏸")
		estado = estiloAlerta.Render(idioma.T("sin saldo · queda pendiente"))
	case v.estado == "detenida":
		g = estiloAlerta.Render("⏸")
		estado = estiloAlerta.Render(idioma.T("detenida"))
	default:
		g = estiloEspera.Render(spinnerFrames[tick%len(spinnerFrames)])
		estado = estiloApagado.Render(reloj(time.Since(inicio)))
	}
	return "  " + g + " " + estiloTinta.Render(f.ID) + "  " + f.Titulo + "  " + estado + "\n"
}

// intentosTxt formatea el conteo de intentos con el singular correcto:
// "1 intento", no "1 intentos".
func intentosTxt(n int) string {
	if n == 1 {
		return idioma.T("1 intento")
	}
	return strconv.Itoa(n) + idioma.T(" intentos")
}
