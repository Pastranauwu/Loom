// Package tui es el modo interactivo de loom: la compuerta
// animada de la esclusa de salida, el tablero y la corrida en vivo. Cuando
// la salida es una terminal y no hay --plain ni --json, los comandos usan
// esta vista; si no, el texto plano de internal/ui.
package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/ship"
)

// NombresPasos es el orden de la compuerta, tal como los emite
// internal/ship. Tiene que coincidir con los Paso{} de ship.go: la lista
// se quedó en ocho mientras el código creció a once, y como el mapeo era
// por índice, el paso "interfaces" se pintaba bajo la etiqueta "bisec" y
// el contador llegaba a 9/8.
var NombresPasos = []string{
	"base", "historial", "ruido", "secretos", "presupuesto",
	"interfaces", "dependencias", "bisectable", "suite_oculta",
	"handoff", "pr",
}

// nombresCortos son las abreviaturas que se pintan bajo los glifos; el
// nombre completo vive en ship.Paso.Nombre y en el JSON. Mismo largo y
// mismo orden que NombresPasos — lo verifica TestEtiquetasAlineadas.
var nombresCortos = []string{
	"base", "hist", "ruido", "secr", "presu",
	"iface", "deps", "bisec", "oculta",
	"hand", "pr",
}

// noAplica marca un paso que esta corrida no ejecutó porque no tocaba
// (sin suite sellada, sin dependencias declaradas). No es un fallo ni
// algo pendiente: por eso no cuenta para el total.
const noAplica = pendiente

// estadoPaso es el estado de un paso en la compuerta.
type estadoPaso int

const (
	pendiente estadoPaso = iota
	trabajando
	verde
	rojo
)

// clasificar devuelve el estado de cada paso de la compuerta dado lo que
// ya terminó. Mientras corre, el siguiente sin resolver queda
// "trabajando".
//
// El emparejamiento es por NOMBRE, no por posición: la compuerta se salta
// pasos que no aplican (suite_oculta sin suite sellada, dependencias sin
// declarar), así que el tercer Paso que llega no tiene por qué ser el
// tercero de la lista. Mapeando por índice, un salto corría todas las
// etiquetas siguientes.
func clasificar(pasos []ship.Paso, terminado bool) []estadoPaso {
	e := make([]estadoPaso, len(NombresPasos))
	indice := make(map[string]int, len(NombresPasos))
	for i, n := range NombresPasos {
		indice[n] = i
	}
	ultimo := -1
	for _, p := range pasos {
		i, conocido := indice[p.Nombre]
		if !conocido {
			continue // paso de otro camino (multitarea): no va en esta fila
		}
		if p.OK {
			e[i] = verde
		} else {
			e[i] = rojo
		}
		if i > ultimo {
			ultimo = i
		}
	}
	if !terminado {
		for i := ultimo + 1; i < len(e); i++ {
			if e[i] == pendiente {
				e[i] = trabajando
				break
			}
		}
	}
	return e
}

// hechosYTotal cuenta para la barra. Mientras la compuerta corre el total
// es cuántos pasos puede haber; al terminar, cuántos hubo de verdad — así
// una corrida que se saltó un paso que no aplicaba cierra en 9/9 y no en
// 9/11, que se lee como si faltara algo.
func hechosYTotal(e []estadoPaso, terminado bool) (int, int) {
	hechos := 0
	for _, s := range e {
		if s == verde || s == rojo {
			hechos++
		}
	}
	if terminado {
		return hechos, hechos
	}
	return hechos, len(e)
}

func renderGlifo(e estadoPaso, tick int) string {
	switch e {
	case verde:
		return estiloPresion.Render("✓")
	case rojo:
		return estiloAlerta.Render("✗")
	case trabajando:
		return estiloEspera.Render(spinnerFrames[tick%len(spinnerFrames)])
	default:
		return estiloApagado.Render("·")
	}
}

func renderNombre(e estadoPaso, nombre string) string {
	switch e {
	case verde:
		return estiloPresion.Render(nombre)
	case rojo:
		return estiloAlerta.Render(nombre)
	case trabajando:
		return estiloEspera.Render(nombre)
	default:
		return estiloApagado.Render(nombre)
	}
}

// anchoPaso son las columnas de cada paso en la fila horizontal.
const anchoPaso = 8

// renderGate dibuja la compuerta con colores, spinner y barra de progreso,
// dentro de una tarjeta. La fila de pasos mide 94 columnas con la tarjeta:
// en una terminal más angosta se partía en dos renglones desalineados, así
// que ahí los pasos van en lista. Si la terminal es baja, sin logo.
// width o alto en 0 = sin límite.
func renderGate(id string, pasos []ship.Paso, terminado bool, tick, width, alto int) string {
	e := clasificar(pasos, terminado)
	const marco = 6 // borde y relleno de la tarjeta

	var cuerpo strings.Builder
	cuerpo.WriteString(estiloTitulo.Render("ESCLUSA DE SALIDA · "+id) + "\n\n")

	if width > 0 && width < len(e)*anchoPaso+marco {
		for i := range e {
			cuerpo.WriteString(renderGlifo(e[i], tick) + " " + renderNombre(e[i], NombresPasos[i]) + "\n")
		}
	} else {
		var glifos, nombres strings.Builder
		for i := 0; i < len(e); i++ {
			glifos.WriteString(acomodar(renderGlifo(e[i], tick), anchoPaso))
			nombres.WriteString(acomodar(renderNombre(e[i], nombresCortos[i]), anchoPaso))
		}
		cuerpo.WriteString(strings.TrimRight(glifos.String(), " ") + "\n")
		cuerpo.WriteString(strings.TrimRight(nombres.String(), " ") + "\n")
	}

	hechos, total := hechosYTotal(e, terminado)
	largo := 40
	if width > 0 {
		largo = max(min(largo, width-marco-6), 5)
	}
	cuerpo.WriteString("\n" + barra(hechos, total, largo) + " " +
		estiloApagado.Render(strconv.Itoa(hechos)+"/"+strconv.Itoa(total)) + "\n")

	if len(pasos) > 0 {
		// el detalle es texto libre (un error de git, una ruta): en una
		// sola línea, cortado, o ensancha la tarjeta fuera de la terminal
		detalle := strings.Join(strings.Fields(pasos[len(pasos)-1].Detalle), " ")
		if r := []rune(detalle); width > marco+1 && len(r) > width-marco {
			detalle = string(r[:width-marco-1]) + "…"
		}
		cuerpo.WriteString("\n" + estiloApagado.Render(detalle) + "\n")
	}

	tarjeta := caja(strings.TrimRight(cuerpo.String(), "\n"))
	logo := Logo(width) + "\n"
	if alto > 0 && strings.Count(logo+tarjeta, "\n")+1 > alto {
		return tarjeta
	}
	return logo + tarjeta
}

// acomodar rellena s con espacios hasta el ancho visible n (celdas).
func acomodar(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// CorrerGate corre la esclusa de salida dentro de la compuerta animada y
// devuelve el resultado cuando termina.
func CorrerGate(o ship.Opciones) (ship.Resultado, error) {
	ch := make(chan gateMsg)
	o.Progreso = func(p ship.Paso) { ch <- gateMsg{paso: p} }
	go func() {
		res := ship.Run(context.Background(), o)
		ch <- gateMsg{fin: true, res: res}
		close(ch)
	}()

	m := gateModel{opciones: o, ch: ch}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return ship.Resultado{}, err
	}
	return final.(gateModel).resultado, nil
}

// gateMsg es un evento de la compuerta: un paso que terminó, o el final.
type gateMsg struct {
	paso ship.Paso
	fin  bool
	res  ship.Resultado
}

type tickMsg struct{}

type gateModel struct {
	opciones  ship.Opciones
	ch        chan gateMsg
	pasos     []ship.Paso
	terminado bool
	resultado ship.Resultado
	tick      int
	ancho     int
	alto      int
}

func (m gateModel) Init() tea.Cmd {
	return tea.Batch(m.escuchar(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m gateModel) escuchar() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.ch
		if !ok {
			return tea.Quit()
		}
		return msg
	}
}

func (m gateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.terminado {
			return m, nil
		}
		m.tick++
		return m, tickCmd()
	case tea.WindowSizeMsg:
		m.ancho, m.alto = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case gateMsg:
		if msg.fin {
			m.terminado = true
			m.resultado = msg.res
			// deja ver la compuerta en verde un instante antes de salir
			return m, tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
		}
		m.pasos = append(m.pasos, msg.paso)
		return m, m.escuchar()
	}
	return m, nil
}

func (m gateModel) View() string {
	return renderGate(m.opciones.Task.ID, m.pasos, m.terminado, m.tick, m.ancho, m.alto)
}
