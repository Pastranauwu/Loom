package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
)

// separador es lo que corre en los paneles de una celda que hacen el marco:
// tmux no dibuja borde por fuera, así que a la izquierda y a la derecha va
// un panel mudo cuyo borde con los demás es la línea del marco (arriba y
// abajo lo hacen la cabecera y la base).
const separador = "printf '\\033[?25l'; exec tail -f /dev/null"

// Títulos con que se marca la orientación de cada separador.
const (
	sepVertical   = "loom-sep-v"
	sepHorizontal = "loom-sep-h"
)

// separar crea un separador con split-window y estos argumentos, lo marca
// con su orientación y le quita la entrada.
func separar(orientacion string, args ...string) error {
	id, err := tmuxID(append(append([]string{"split-window", "-d"}, args...), separador)...)
	if err != nil {
		return err
	}
	// select-pane con -d sale antes de mirar -T: van en dos llamadas
	_ = tmux("select-pane", "-t", id, "-d")
	return tmux("select-pane", "-t", id, "-T", orientacion)
}

// gancho pone un hook en la sesión s. window-resized y pane-focus-in son de
// ventana: a nivel de sesión tmux los rechaza (y el error se perdía).
func gancho(s, nombre, cmd string) {
	args := []string{"set-hook", "-t", s}
	if nombre == "window-resized" || nombre == "pane-focus-in" {
		args = []string{"set-hook", "-w", "-t", s}
	}
	if cmd == "" {
		args = append(args, "-u", nombre)
	} else {
		args = append(args, nombre, cmd)
	}
	_ = tmux(args...)
}

func tmuxID(args ...string) (string, error) {
	full := append([]string{args[0], "-P", "-F", "#{pane_id}"}, args[1:]...)
	o, err := exec.Command("tmux", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s", args[0], strings.TrimSpace(string(o)))
	}
	return strings.TrimSpace(string(o)), nil
}

// panelesIDE ubica cada panel de la sesión por lo que corre: los índices
// cambian con cada separador.
type panelesIDE struct {
	claude, nav, uso     string
	cabecera, barra      string   // las filas de arriba y abajo (panel cabecera, panel barra)
	fondo                string   // con cajas flotantes: cabecera y base en un solo panel debajo
	separadores, paneles []string // separadores: una celda; paneles: lo que corre loom panel
	anchos, altos        []string // separadores verticales y horizontales
}

func leerPaneles(s string) panelesIDE {
	var p panelesIDE
	o, _ := exec.Command("tmux", "list-panes", "-t", s, "-F", "#{pane_id}\t#{pane_left}\t#{pane_height}\t#{pane_start_command}\t#{pane_title}").Output()
	for _, l := range strings.Split(strings.TrimSpace(string(o)), "\n") {
		c := strings.SplitN(l, "\t", 5)
		if len(c) < 5 {
			continue
		}
		id, cmd := c[0], c[3]
		switch {
		case strings.Contains(cmd, "tail -f /dev/null"):
			p.separadores = append(p.separadores, id)
			// la orientación va en el título al crearlo (separar): por el
			// tamaño no se sabe, un separador puede crecer al cerrar un panel
			if c[4] == sepHorizontal {
				p.altos = append(p.altos, id)
			} else {
				p.anchos = append(p.anchos, id)
			}
		case strings.Contains(cmd, "panel barra"):
			p.barra = id
			p.paneles = append(p.paneles, id)
		case strings.Contains(cmd, "panel fondo"):
			p.fondo = id
			p.paneles = append(p.paneles, id)
		case strings.Contains(cmd, "panel cabecera"):
			p.cabecera = id
			p.paneles = append(p.paneles, id)
		case strings.Contains(cmd, "panel nav"):
			p.nav = id
			p.paneles = append(p.paneles, id)
		case strings.Contains(cmd, "panel usage"):
			p.uso = id
			p.paneles = append(p.paneles, id)
		default:
			p.claude = id
		}
	}
	return p
}

// estiloIDE pinta la sesión con el tema actual: bordes dobles, barra con
// las teclas en chips y F1 de ayuda. Corre al crear la sesión y al cambiar
// de tema con la sesión abierta.
func estiloIDE(s, root, exe string) {
	t := tui.TemaActual()
	p := leerPaneles(s)
	opciones := [][]string{
		// sin línea de título: con ella tmux no cuenta el borde de abajo
		// como del panel activo (es el título del de abajo) y la caja con
		// foco quedaba abierta. Sin ella ilumina sus cuatro lados exactos;
		// los títulos van en la cabecera y dentro de cada panel
		{"pane-border-status", "off"},
		{"pane-border-lines", "double"},
		{"pane-border-style", "fg=" + t.Borde + ",bg=" + t.Fondo},
		{"pane-active-border-style", "fg=" + t.Acento + ",bg=" + t.Fondo},
		{"window-style", "fg=" + t.Tinta + ",bg=" + t.Fondo},
		{"window-active-style", "fg=" + t.Tinta + ",bg=" + t.Fondo},
		// la barra de estado la dibujan cabecera y base (panel barra): así
		// no queda una fila suelta fuera del marco
		{"status", "off"},
	}
	for _, o := range opciones {
		_ = tmux("set-option", "-t", s, o[0], o[1])
	}
	// los iconos Nerd viven en el área de uso privado y una fuente Mono los
	// dibuja en una celda; tmux los contaba de dos y corría los bordes de las
	// tarjetas. Es opción del servidor (tmux 3.5+); en uno viejo no hace nada
	_ = tmux("set-option", "-s", "codepoint-widths[0]", "U+E000-U+F8FF=1")
	_ = tmux("set-option", "-s", "codepoint-widths[1]", "U+F0000-U+FFFFD=1")

	// las teclas de función solo actúan en las sesiones de loom: en
	// cualquier otra sesión tmux siguen llegando a la aplicación
	popup := func(w, h, titulo, cmd string) string {
		return fmt.Sprintf("display-popup -E -w %s -h %s -T %s -S fg=%s %s", w, h, comillas(" "+titulo+" "), t.Acento, comillas(cmd))
	}
	enLoom := "#{m:loom-*,#{session_name}}"
	for _, b := range [][2]string{
		{"F1", popup("62", "16", idioma.T("ayuda · cualquier tecla sale"), comillas(exe)+" panel ayuda")},
		{"F2", "resize-pane -Z"},
		{"F3", popup("90%", "90%", idioma.T("tablero · q sale"), "cd "+comillas(root)+" && "+comillas(exe)+" board --still")},
		{"F4", popup("40", "22", idioma.T("tema · esc sale"), "cd "+comillas(root)+" && "+comillas(exe)+" theme")},
		{"F12", "detach-client"},
	} {
		_ = tmux("bind-key", "-n", b[0], "if-shell", "-F", enLoom, b[1], "send-keys "+b[0])
	}
	// ctrl+b ctrl+d: soltar ctrl a tiempo no es obvio, y sin esto no pasaba nada
	_ = tmux("bind-key", "-T", "prefix", "C-d", "detach-client")

	gancho(s, "pane-focus-in", "")
	// fondo, cabecera, base y márgenes son decorado: un clic en ellos
	// devuelve el foco a claude, si no se ilumina una caja que no es
	gancho(s, "after-select-pane", "")
	if p.claude != "" {
		decorado := "#{m:*tail -f /dev/null*,#{pane_start_command}}"
		for _, n := range []string{"fondo", "cabecera", "barra"} {
			decorado = "#{||:" + decorado + ",#{m:*panel " + n + "*,#{pane_start_command}}}"
		}
		gancho(s, "after-select-pane", "if-shell -F '"+decorado+"' 'select-pane -t "+p.claude+"'")
	}
	if p.fondo != "" {
		acomodarCajas(s, p)
		// la cuenta la hace loom: tmux no reparte las cajas flotantes
		ajuste := "run-shell -b -c " + comillas(root) + " " + comillas(comillas(exe)+" panel ajustar")
		for _, h := range []string{"client-resized", "window-resized"} {
			gancho(s, h, ajuste)
		}
	} else {
		ajustarIDE(s, p)
	}
	if p.claude != "" {
		_ = tmux("select-pane", "-t", p.claude)
	}
}

// ajustarIDE deja separadores en una celda, claude en dos tercios y el consumo en 6 filas, ahora y cada vez que la terminal cambia
// de tamaño. El orden importa: tmux da lo que encoge al vecino, así que
// el navegador crece primero y después el consumo y los separadores le
// devuelven solo lo suyo.
func ajustarIDE(s string, p panelesIDE) {
	var cmds []string
	// la base primero: lo que suelta va a la fila de arriba entera, y
	// después navegador y consumo se reparten solo lo suyo
	for _, id := range []string{p.barra, p.cabecera} {
		if id != "" {
			cmds = append(cmds, "resize-pane -t "+id+" -y 1")
		}
	}
	angostar := func() {
		for _, id := range p.anchos {
			cmds = append(cmds, "resize-pane -t "+id+" -x 1")
		}
	}
	// claude va a dos tercios; el navegador, a la derecha, toma el resto
	angostar()
	if p.claude != "" {
		cmds = append(cmds, "resize-pane -t "+p.claude+" -x 64%")
	}
	angostar()
	if p.nav != "" {
		cmds = append(cmds, "resize-pane -t "+p.nav+" -y 999")
	}
	if p.uso != "" {
		cmds = append(cmds, "resize-pane -t "+p.uso+" -y 6")
	}
	for _, id := range p.altos {
		cmds = append(cmds, "resize-pane -t "+id+" -y 1")
	}
	if len(cmds) == 0 {
		return
	}
	ajuste := strings.Join(cmds, " ; ")
	// client-resized corre antes de que la ventana tome el tamaño nuevo, y
	// tmux le da lo que crece a los separadores: window-resized va después
	for _, h := range []string{"client-resized", "window-resized"} {
		gancho(s, h, ajuste)
	}
	// ";" suelto es el separador de comandos de tmux, no del shell
	_ = exec.Command("tmux", strings.Fields(ajuste)...).Run()
}

// usoCaja es la altura de la caja del consumo: título, cinco filas y bordes.
const usoCaja = 8

// cajasIDE reparte la ventana de w×h entre las cajas flotantes, cada una
// como {x, y, ancho, alto} con su borde: claude a la izquierda en dos
// tercios, el navegador arriba a la derecha y el consumo abajo. La fila de
// arriba y la de abajo quedan para la cabecera y la base del fondo.
func cajasIDE(w, h int) (claude, nav, uso [4]int) {
	cw := w * 64 / 100
	alto := max(h-2, 3)
	usoH := min(usoCaja, max(alto-3, 0))
	claude = [4]int{0, 1, cw, alto}
	nav = [4]int{cw, 1, w - cw, alto - usoH}
	uso = [4]int{cw, 1 + alto - usoH, w - cw, usoH}
	return
}

// acomodarCajas pone cada caja flotante en su lugar según el tamaño de la
// ventana. Con un panel agrandado (F2) no toca nada.
func acomodarCajas(s string, p panelesIDE) {
	o, err := exec.Command("tmux", "display-message", "-p", "-t", s, "#{window_width} #{window_height} #{window_zoomed_flag}").Output()
	var w, h, zoom int
	if err != nil {
		return
	}
	if n, _ := fmt.Sscan(string(o), &w, &h, &zoom); n < 3 || zoom == 1 {
		return
	}
	c, n, u := cajasIDE(w, h)
	var args []string
	for _, caja := range []struct {
		id string
		r  [4]int
	}{{p.claude, c}, {p.nav, n}, {p.uso, u}} {
		if caja.id == "" || caja.r[2] < 3 || caja.r[3] < 3 {
			continue
		}
		args = append(args, "resize-pane", "-t", caja.id, "-x", strconv.Itoa(caja.r[2]), "-y", strconv.Itoa(caja.r[3]), ";",
			"move-pane", "-t", caja.id, "-X", strconv.Itoa(caja.r[0]), "-Y", strconv.Itoa(caja.r[1]), ";")
	}
	if len(args) > 0 {
		_ = exec.Command("tmux", args...).Run()
	}
}
