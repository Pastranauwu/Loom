package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// paleta del cuarto limpio industrial
var (
	presion = lipgloss.Color("#4FB3A2")
	alerta  = lipgloss.Color("#D96C4A")
	espera  = lipgloss.Color("#C9A227")
	apagado = lipgloss.Color("#6B6F72")
	tinta   = lipgloss.Color("#E6E6E1")
)

var (
	estiloPresion = lipgloss.NewStyle().Foreground(presion)
	estiloAlerta  = lipgloss.NewStyle().Foreground(alerta)
	estiloEspera  = lipgloss.NewStyle().Foreground(espera)
	estiloApagado = lipgloss.NewStyle().Foreground(apagado)
	estiloTinta   = lipgloss.NewStyle().Foreground(tinta)
	estiloBold    = lipgloss.NewStyle().Foreground(tinta).Bold(true)
	estiloTitulo  = lipgloss.NewStyle().Foreground(presion).Bold(true)
)

// spinnerFrames es la animación del trabajo en curso (braille).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// colores de la paleta en RGB, para el sticker del plasma.
var (
	rgbTinta   = [3]int{230, 230, 225}
	rgbPresion = [3]int{79, 179, 162}
	rgbAlerta  = [3]int{217, 108, 74}
	rgbEspera  = [3]int{201, 162, 39}
	rgbApagado = [3]int{107, 111, 114}
)

// logoFilas es el logotipo de loom en arte de píxeles (fuente
// unsciithin de bit), sin colores; el degradado se aplica aquí.
var logoFilas = []string{
	"    █                   ▀█",
	"▄▀▀▀█ ▄▀▀▀▄ █   █ ▄▀▀▀▄  █ ▄▀▀▀▄  ▀▀▀▄ █▀▀▀▄",
	"█   █ █▀▀▀▀ ▀▄ ▄▀ █   ▄  █ █▀▀▀▀ ▄▀▀▀█ █   █",
	" ▀▀▀▀  ▀▀▀    ▀    ▀▀▀  ▀▀▀ ▀▀▀   ▀▀▀▀ ▀   ▀",
}

// mezclar interpola dos colores RGB y devuelve su hex.
func mezclar(a, b [3]int, t float64) string {
	c := mezclarRGB(a, b, t)
	return "#" + hex2(c[0]) + hex2(c[1]) + hex2(c[2])
}

// mezclarRGB interpola dos colores RGB.
func mezclarRGB(a, b [3]int, t float64) [3]int {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return [3]int{
		a[0] + int(float64(b[0]-a[0])*t),
		a[1] + int(float64(b[1]-a[1])*t),
		a[2] + int(float64(b[2]-a[2])*t),
	}
}

func hex2(n int) string {
	const d = "0123456789abcdef"
	return string([]byte{d[n/16], d[n%16]})
}

// Logo devuelve el logotipo con degradado vertical, indentado.
func Logo(width int) string {
	alto := len(logoFilas)
	inicio := [3]int{0x4F, 0xB3, 0xA2} // presion #4FB3A2
	final := [3]int{0x2C, 0x6E, 0x63}  // teal profundo #2C6E63

	var b strings.Builder
	for i, fila := range logoFilas {
		c := mezclar(inicio, final, float64(i)/float64(alto-1))
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(fila) + "\n")
	}
	b.WriteString("\n  " + estiloApagado.Render("dirige agentes · entrega código limpio") + "\n")
	return b.String()
}

// Caja envuelve contenido en una tarjeta con borde recto, para
// comandos fuera de internal/tui que quieren la misma tarjeta.
func Caja(s string) string { return caja(s) }

// Titulo y Apagado exponen los dos estilos de texto más usados fuera de
// este paquete, sin tener que exportar la paleta entera.
func Titulo(s string) string  { return estiloTitulo.Render(s) }
func Apagado(s string) string { return estiloApagado.Render(s) }
func Presion(s string) string { return estiloPresion.Render(s) }
func Alerta(s string) string  { return estiloAlerta.Render(s) }
func Espera(s string) string  { return estiloEspera.Render(s) }

// caja envuelve contenido en una tarjeta con borde recto.
func caja(s string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(apagado).
		Padding(1, 2).
		Render(s)
}

// barra dibuja una barra de progreso real (hecho/total), nunca inventada.
func barra(hecho, total, ancho int) string {
	if total <= 0 {
		total = 1
	}
	lleno := ancho * hecho / total
	if lleno < 0 {
		lleno = 0
	}
	if lleno > ancho {
		lleno = ancho
	}
	return estiloPresion.Render(strings.Repeat("█", lleno)) +
		estiloApagado.Render(strings.Repeat("░", ancho-lleno))
}

// reloj formatea una duración como 1m14s o 3s.
func reloj(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return strconv.Itoa(s) + "s"
	}
	return strconv.Itoa(s/60) + "m" + strconv.Itoa(s%60) + "s"
}
