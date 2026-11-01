package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tema es una paleta completa: la TUI, los bordes y la barra de tmux y el
// fondo de loom ide salen de aquí. Son paletas publicadas, no inventadas:
// cada una ya combina consigo misma.
type Tema struct {
	Nombre     string
	Fondo      string // fondo de los paneles
	Superficie string // barras vacías, chips apagados, barra de estado
	Borde      string // bordes inactivos
	Apagado    string // texto secundario
	Tinta      string // texto principal
	Acento     string // marca, borde activo, progreso
	Alerta     string
	Espera     string
}

// Temas disponibles: los de fábrica (el primero es el de siempre) y, al
// final, los propios de ~/.loom/themes/ (CargarTemasPropios).
var Temas = []Tema{
	{"loom", "#141618", "#2a2e31", "#3a3f42", "#6B6F72", "#E6E6E1", "#4FB3A2", "#D96C4A", "#C9A227"},
	{"nord", "#2E3440", "#3B4252", "#4C566A", "#7B88A1", "#ECEFF4", "#88C0D0", "#BF616A", "#EBCB8B"},
	{"gruvbox", "#282828", "#3c3836", "#504945", "#928374", "#ebdbb2", "#8ec07c", "#fb4934", "#fabd2f"},
	{"tokyo-night", "#1a1b26", "#24283b", "#414868", "#565f89", "#c0caf5", "#7aa2f7", "#f7768e", "#e0af68"},
	{"catppuccin", "#1e1e2e", "#313244", "#45475a", "#7f849c", "#cdd6f4", "#cba6f7", "#f38ba8", "#f9e2af"},
	{"rose-pine", "#191724", "#1f1d2e", "#403d52", "#6e6a86", "#e0def4", "#ebbcba", "#eb6f92", "#f6c177"},
}

var temaActual = Temas[0]

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func dirTemas() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".loom", "themes")
}

// colores devuelve los campos de color del tema, por nombre.
func (t *Tema) colores() map[string]*string {
	return map[string]*string{"background": &t.Fondo, "surface": &t.Superficie, "border": &t.Borde, "muted": &t.Apagado,
		"text": &t.Tinta, "accent": &t.Acento, "alert": &t.Alerta, "warning": &t.Espera}
}

// NuevoTema arma un tema a partir de base cambiando los colores dados
// (background, surface, border, muted, text, accent, alert, warning).
func NuevoTema(nombre, base string, cambios map[string]string) (Tema, error) {
	nombre = strings.ToLower(strings.TrimSpace(nombre))
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(nombre) {
		return Tema{}, fmt.Errorf("nombre de tema inválido %q · letras, números y guiones", nombre)
	}
	t, ok := BuscarTema(base)
	if !ok {
		t = Temas[0]
	}
	t.Nombre = nombre
	campos := t.colores()
	for k, v := range cambios {
		dst, ok := campos[k]
		if !ok {
			return Tema{}, fmt.Errorf("color desconocido %q · usa background, surface, border, muted, text, accent, alert o warning", k)
		}
		if !hexColor.MatchString(v) {
			return Tema{}, fmt.Errorf("%s=%q no es un color #RRGGBB", k, v)
		}
		*dst = v
	}
	return t, nil
}

// GuardarTemaPropio escribe el tema en ~/.loom/themes/<nombre>.json y lo
// suma a Temas.
func GuardarTemaPropio(t Tema) error {
	for _, f := range Temas[:len(fabrica)] {
		if f.Nombre == t.Nombre {
			return fmt.Errorf("%s es un tema de fábrica · elige otro nombre", t.Nombre)
		}
	}
	m := map[string]string{"name": t.Nombre}
	for k, v := range t.colores() {
		m[k] = *v
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(dirTemas(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dirTemas(), t.Nombre+".json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	CargarTemasPropios()
	return nil
}

var fabrica = append([]Tema(nil), Temas...)

// CargarTemasPropios rehace Temas con los de fábrica más los JSON válidos
// de ~/.loom/themes/. Uno ilegible se salta.
func CargarTemasPropios() {
	Temas = append([]Tema(nil), fabrica...)
	archivos, _ := filepath.Glob(filepath.Join(dirTemas(), "*.json"))
	for _, a := range archivos {
		data, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		var m map[string]string
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		nombre := strings.TrimSuffix(filepath.Base(a), ".json")
		delete(m, "name")
		if t, err := NuevoTema(nombre, "loom", m); err == nil {
			if _, ya := BuscarTema(t.Nombre); !ya {
				Temas = append(Temas, t)
			}
		}
	}
}

// BuscarTema devuelve el tema de ese nombre.
func BuscarTema(nombre string) (Tema, bool) {
	for _, t := range Temas {
		if t.Nombre == strings.ToLower(strings.TrimSpace(nombre)) {
			return t, true
		}
	}
	return Tema{}, false
}

// TemaActual es el tema con que se está pintando.
func TemaActual() Tema { return temaActual }

// AplicarTema pinta con el tema de ese nombre; uno desconocido deja el de loom.
func AplicarTema(nombre string) {
	if len(Temas) == len(fabrica) {
		CargarTemasPropios()
	}
	t, ok := BuscarTema(nombre)
	if !ok {
		t = Temas[0]
	}
	temaActual = t
	presion, alerta, espera = lipgloss.Color(t.Acento), lipgloss.Color(t.Alerta), lipgloss.Color(t.Espera)
	apagado, tinta = lipgloss.Color(t.Apagado), lipgloss.Color(t.Tinta)
	superficie, borde, fondo = lipgloss.Color(t.Superficie), lipgloss.Color(t.Borde), lipgloss.Color(t.Fondo)
	estiloPresion = lipgloss.NewStyle().Foreground(presion)
	estiloAlerta = lipgloss.NewStyle().Foreground(alerta)
	estiloEspera = lipgloss.NewStyle().Foreground(espera)
	estiloApagado = lipgloss.NewStyle().Foreground(apagado)
	estiloTinta = lipgloss.NewStyle().Foreground(tinta)
	estiloBold = lipgloss.NewStyle().Foreground(tinta).Bold(true)
	estiloTitulo = lipgloss.NewStyle().Foreground(presion).Bold(true)
	rgbTinta, rgbPresion, rgbAlerta = rgb(t.Tinta), rgb(t.Acento), rgb(t.Alerta)
	rgbEspera, rgbApagado = rgb(t.Espera), rgb(t.Apagado)
	neon = paletaPlasma(t)
}

// paletaPlasma va del fondo del tema a su acento y de ahí a la tinta: el
// plasma del tablero se ve del mismo tema que el resto.
func paletaPlasma(t Tema) [][3]int {
	f, s, a, k := rgb(t.Fondo), rgb(t.Superficie), rgb(t.Acento), rgb(t.Tinta)
	return [][3]int{f, mezclarRGB(f, s, 0.6), s, mezclarRGB(s, a, 0.4), mezclarRGB(s, a, 0.75), a, mezclarRGB(a, k, 0.45), mezclarRGB(a, k, 0.8)}
}

func rgb(hex string) [3]int {
	n, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return [3]int{int(n >> 16 & 0xff), int(n >> 8 & 0xff), int(n & 0xff)}
}

func archivoTema() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".loom", "theme")
}

// TemaPreferido es el tema que eligió el usuario para todos sus proyectos,
// o "" si nunca eligió.
func TemaPreferido() string {
	b, err := os.ReadFile(archivoTema())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// GuardarTema guarda el tema del usuario en ~/.loom/theme.
func GuardarTema(nombre string) error {
	p := archivoTema()
	if _, ok := BuscarTema(nombre); !ok || p == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(nombre+"\n"), 0o644)
}

// Muestra pinta el tema en una línea: sus colores como bloques.
func Muestra(t Tema) string {
	var b strings.Builder
	for _, c := range []string{t.Fondo, t.Superficie, t.Borde, t.Apagado, t.Tinta, t.Acento, t.Alerta, t.Espera} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("██"))
	}
	return b.String()
}

// BarraIDE es la base de loom ide, una fila entre las líneas del marco:
// loom a la izquierda y las teclas a la derecha.
func BarraIDE(ancho int, ayuda, ocultar string) string {
	chip := func(tecla, que string) string {
		return lipgloss.NewStyle().Background(presion).Foreground(fondo).Bold(true).Render(" "+tecla+" ") + estiloApagado.Render(" "+que+" ")
	}
	izq := estiloTitulo.Render(" loom")
	der := chip("F1", ayuda) + " " + chip("F12", ocultar)
	hueco := max(ancho-lipgloss.Width(izq)-lipgloss.Width(der), 1)
	return izq + strings.Repeat(" ", hueco) + der
}

// CabeceraIDE es la fila de arriba de loom ide: el título de claude a la
// izquierda y los agentes activos a la derecha.
func CabeceraIDE(ancho int, titulo, derecha string) string {
	izq := estiloTitulo.Render(" ✱ " + titulo)
	der := estiloTitulo.UnsetBold().Render(derecha + " ")
	hueco := max(ancho-lipgloss.Width(izq)-lipgloss.Width(der), 1)
	return lipgloss.NewStyle().MaxWidth(ancho).Render(izq + strings.Repeat(" ", hueco) + der)
}

// TituloPanel es la primera fila de un panel de loom ide, en apagado.
func TituloPanel(t string) string { return estiloApagado.Render(t) }
