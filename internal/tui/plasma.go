package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PlasmaParams ajusta el look de la lámpara de lava.
type PlasmaParams struct {
	Speed      float64 // velocidad de la animación
	Scale      float64 // tamaño de los blobs
	Distortion float64 // deformación (warp tipo lava)
	Contrast   float64 // dureza de los bordes
	// Quieto deja el fondo liso con el color del tema: sin animación. En
	// una ventana flotante de tmux el plasma parpadeaba en cada fotograma.
	Quieto bool
}

// DefaultPlasma es un equilibrio tipo lámpara de lava, relajado.
func DefaultPlasma() PlasmaParams {
	return PlasmaParams{Speed: 0.5, Scale: 1.4, Distortion: 0.7, Contrast: 1.4}
}

// neon es la paleta del plasma, de oscuro a pálido. AplicarTema la rehace
// con los colores del tema (paletaPlasma).
var neon = [][3]int{
	{4, 6, 20},
	{6, 26, 32},
	{10, 62, 46},
	{16, 110, 70},
	{32, 165, 98},
	{58, 205, 124},
	{112, 232, 155},
	{190, 248, 210},
}

// rgbNeon interpola la paleta en v (0..1).
func rgbNeon(v float64) [3]int {
	p := v * float64(len(neon)-1)
	i := int(p)
	if i > len(neon)-2 {
		i = len(neon) - 2
	}
	f := p - float64(i)
	a, b := neon[i], neon[i+1]
	return [3]int{
		int(float64(a[0]) + (float64(b[0])-float64(a[0]))*f),
		int(float64(a[1]) + (float64(b[1])-float64(a[1]))*f),
		int(float64(a[2]) + (float64(b[2])-float64(a[2]))*f),
	}
}

// campo evalúa el plasma en el punto (x, y) de un campo de w×h.
func campo(x, y, t, w, h, escala, distorsion, contraste float64) float64 {
	s := 14.0 * escala
	fx := x / s
	fy := y / s
	fx += math.Sin(fy*1.7+t*0.6) * distorsion
	v := math.Sin(fx+t) +
		math.Sin(fy*0.9-t*0.7) +
		math.Sin((fx+fy)*0.5+t*1.3) +
		math.Sin(math.Hypot(fx-w/(2*s), fy-h/(2*s))*1.4-t)
	v = (v + 4) / 8
	v = (v-0.5)*contraste + 0.5
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v
}

// celda es una celda de la terminal. fg/bg nil = reset (fondo transparente).
type celda struct {
	fg *[3]int
	bg *[3]int
	ch string
}

// plasmaGrid calcula el campo completo para una terminal de ancho×alto,
// usando medio bloque ▀ (fg = fila par, bg = fila impar).
func plasmaGrid(ancho, alto int, t float64, p PlasmaParams) [][]celda {
	grid := make([][]celda, alto)
	if p.Quieto {
		liso := neon[0]
		for row := range grid {
			grid[row] = make([]celda, ancho)
			for col := range grid[row] {
				grid[row][col] = celda{bg: &liso, ch: " "}
			}
		}
		return grid
	}
	w := float64(ancho)
	h := float64(alto * 2)
	for row := 0; row < alto; row++ {
		grid[row] = make([]celda, ancho)
		for col := 0; col < ancho; col++ {
			top := rgbNeon(campo(float64(col), float64(row*2), t, w, h, p.Scale, p.Distortion, p.Contrast))
			bot := rgbNeon(campo(float64(col), float64(row*2+1), t, w, h, p.Scale, p.Distortion, p.Contrast))
			grid[row][col] = celda{fg: &top, bg: &bot, ch: "▀"}
		}
	}
	return grid
}

// lineaSticker es una línea de contenido del sticker, con su color.
type lineaSticker struct {
	texto string
	color [3]int
}

// renderGrid emite la grilla como ANSI, agrupando celdas del mismo color.
// Sin salto de línea final: bubbletea parpadea si el frame termina en \n.
func renderGrid(grid [][]celda) string {
	var b strings.Builder
	for i, row := range grid {
		if i > 0 {
			b.WriteString("\n")
		}
		var cf, cb *[3]int
		for _, c := range row {
			if c.fg != nil {
				if cf == nil || *cf != *c.fg {
					fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", c.fg[0], c.fg[1], c.fg[2])
					cf = c.fg
				}
			} else if cf != nil {
				b.WriteString("\x1b[39m")
				cf = nil
			}
			if c.bg != nil {
				if cb == nil || *cb != *c.bg {
					fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", c.bg[0], c.bg[1], c.bg[2])
					cb = c.bg
				}
			} else if cb != nil {
				b.WriteString("\x1b[49m")
				cb = nil
			}
			b.WriteString(c.ch)
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// FondoPlasma devuelve un fotograma de plasma con el contenido centrado
// como sticker: un margen transparente alrededor para que el logo no se
// pise con el fondo.
func FondoPlasma(ancho, alto int, t float64, p PlasmaParams, lineas []lineaSticker, margen int) string {
	if ancho <= 0 {
		ancho = 80
	}
	if alto <= 0 {
		alto = 24
	}
	grid := plasmaGrid(ancho, alto, t*p.Speed, p)

	maxAncho := 0
	for _, l := range lineas {
		if w := lipgloss.Width(l.texto); w > maxAncho {
			maxAncho = w
		}
	}
	if maxAncho == 0 {
		return renderGrid(grid)
	}
	boxAncho := maxAncho + 2*margen
	boxAlto := len(lineas) + 2*margen
	x0 := (ancho - boxAncho) / 2
	y0 := (alto - boxAlto) / 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}

	// margen transparente (sticker)
	for r := y0; r < y0+boxAlto && r < alto; r++ {
		for c := x0; c < x0+boxAncho && c < ancho; c++ {
			grid[r][c] = celda{ch: " "}
		}
	}
	// texto, por runa para respetar el ancho de glifos como ✓
	for i, l := range lineas {
		r := y0 + margen + i
		if r < 0 || r >= alto {
			continue
		}
		col := l.color
		for j, ru := range []rune(l.texto) {
			c := x0 + margen + j
			if c < 0 || c >= ancho {
				continue
			}
			grid[r][c] = celda{fg: &col, ch: string(ru)}
		}
	}
	return renderGrid(grid)
}

// tickPlasma avanza la animación del fondo. 100ms (10 fps) basta para un
// plasma lento y reduce el parpadeo frente a 20 fps.
func tickPlasma() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}
