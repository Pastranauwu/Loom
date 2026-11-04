package tui

import (
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
)

// Iconos son los glifos de la TUI. Con una Nerd Font salen de ella; sin
// ella, los símbolos Unicode de siempre, que cualquier terminal dibuja.
// Casi todos son del plano básico (FontAwesome, Codicons, Seti); JS y TS
// usan la insignia Material del plano suplementario, porque los otros son
// solo letras. tmux cuenta ambos planos de una celda (estiloIDE).
type Iconos struct {
	Nerd                                  bool
	Lista, EnCurso, Detenida, Pendiente   string
	Contrato, Simbolo, Capa, Tareas, Mapa string
}

var (
	iconosPlano = Iconos{Lista: "✓", EnCurso: "◐", Detenida: "✗", Pendiente: "·", Contrato: "◆", Simbolo: "◇"}
	iconosNerd  = Iconos{Nerd: true,
		Lista: "\uf00c", EnCurso: "\uf110", Detenida: "\uf04c", Pendiente: "\uf10c",
		Contrato: "\ueb2d", Simbolo: "\ueb5b", Capa: "\uebd2", Tareas: "\uf0ae", Mapa: "\uf0e8"}

	// por extensión: el lenguaje de un archivo o del módulo que domina
	iconoLenguaje = map[string]string{
		".go": "\ue627", ".py": "\ue606", ".js": "\U000f031e", ".mjs": "\U000f031e", ".jsx": "\ue7ba",
		".ts": "\U000f06e6", ".tsx": "\ue7ba", ".rs": "\ue7a8", ".html": "\ue60e", ".css": "\ue614",
	}

	iconosUna   sync.Once
	iconosAhora = iconosPlano
)

// ic son los iconos de esta terminal; se deciden una vez por proceso.
func ic() Iconos {
	iconosUna.Do(func() {
		if HayNerdFont() {
			iconosAhora = iconosNerd
		}
	})
	return iconosAhora
}

// HayNerdFont dice si la terminal dibuja glifos Nerd Font. LOOM_ICONS=nerd
// o plain manda; si no, kitty y WezTerm los traen integrados y en el resto
// se busca una Nerd Font instalada con fc-list.
func HayNerdFont() bool {
	switch os.Getenv("LOOM_ICONS") {
	case "nerd":
		return true
	case "plain":
		return false
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "xterm-kitty" || os.Getenv("TERM_PROGRAM") == "WezTerm" {
		return true
	}
	out, err := exec.Command("fc-list", ":", "family").Output()
	return err == nil && strings.Contains(string(out), "Nerd Font")
}

// conIcono antepone un icono y su espacio; sin icono deja el texto igual.
func conIcono(icono, texto string) string {
	if icono == "" {
		return texto
	}
	return icono + " " + texto
}

// iconoDe es el icono del lenguaje de un archivo, o "" sin Nerd Font.
func iconoDe(archivo string) string {
	if !ic().Nerd {
		return ""
	}
	return iconoLenguaje[path.Ext(archivo)]
}

// glifoColumna es el glifo de una columna del tablero: el Nerd de su estado
// o, sin Nerd Font, el Unicode propio de la columna.
func glifoColumna(estado, plano string) string {
	if !ic().Nerd {
		return plano
	}
	return glifoEstado(estado)
}
