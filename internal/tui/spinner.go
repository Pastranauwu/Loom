package tui

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
)

type esperarMsg struct{ err error }

type avanceMsg string

// maxAvances son los últimos avances que se ven bajo el spinner.
const maxAvances = 6

type esperarModel struct {
	titulo  string
	ch      chan error
	tick    int
	err     error
	fin     bool
	inicio  time.Time
	avances []string
}

// Esperar muestra un spinner con `titulo` mientras `trabajo` corre en
// segundo plano. No es una barra de progreso inventada: no se
// sabe cuánto falta, así que solo gira. Devuelve el error de `trabajo`.
func Esperar(titulo string, trabajo func() error) error {
	return EsperarConAvances(titulo, func(func(string)) error { return trabajo() })
}

// EsperarConAvances es Esperar con el tiempo transcurrido y los últimos
// avances que `trabajo` reporte: un planificador de diez minutos que
// solo gira no deja ver si trabaja o alucina.
func EsperarConAvances(titulo string, trabajo func(avance func(string)) error) error {
	ch := make(chan error, 1)
	m := esperarModel{titulo: titulo, ch: ch, inicio: time.Now()}
	p := tea.NewProgram(m)
	go func() { ch <- trabajo(func(s string) { p.Send(avanceMsg(s)) }) }()
	res, err := p.Run()
	if err != nil {
		return err
	}
	return res.(esperarModel).err
}

func (m esperarModel) Init() tea.Cmd {
	return tea.Batch(m.escuchar(), tickCmd())
}

func (m esperarModel) escuchar() tea.Cmd {
	return func() tea.Msg { return esperarMsg{err: <-m.ch} }
}

func (m esperarModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.fin {
			return m, nil
		}
		m.tick++
		return m, tickCmd()
	case avanceMsg:
		m.avances = append(m.avances, string(msg))
		if len(m.avances) > maxAvances {
			m.avances = m.avances[len(m.avances)-maxAvances:]
		}
		return m, nil
	case esperarMsg:
		m.fin = true
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.err = errInterrumpido
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m esperarModel) View() string {
	g := estiloEspera.Render(spinnerFrames[m.tick%len(spinnerFrames)])
	cuerpo := g + " " + estiloTinta.Render(m.titulo) + " " + estiloApagado.Render("· "+Duracion(time.Since(m.inicio)))
	if len(m.avances) > 0 {
		cuerpo += "\n\n" + estiloApagado.Render(strings.Join(m.avances, "\n"))
	}
	return Logo(80) + "\n" + caja(cuerpo)
}

var errInterrumpido = errInterrumpidoT{}

type errInterrumpidoT struct{}

func (errInterrumpidoT) Error() string { return idioma.T("interrumpido") }

// Duracion escribe d al segundo: "4m07s".
func Duracion(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
