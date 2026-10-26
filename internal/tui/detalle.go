package tui

import (
	"github.com/Pastranauwu/loom/internal/idioma"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// detalleTarea arma el panel de detalle de una tarea: su contrato y lo
// que pasó en cada intento. Antes la tecla d salía del tablero a
// `loom logs` y había que volver a abrirlo para mirar la siguiente.
// Lo que no se puede leer se omite: el panel no falla.
func detalleTarea(root, id string, ancho int) []lineaSticker {
	ancho -= 2 * margenTablero
	if ancho < 20 {
		ancho = 76
	}
	var ls []lineaSticker
	campo := func(nombre, valor string, color [3]int) {
		if valor == "" {
			return
		}
		for i, l := range envolver(valor, ancho-14) {
			if i > 0 {
				nombre = ""
			}
			ls = append(ls, lineaSticker{texto: nombre + strings.Repeat(" ", 14-len([]rune(nombre))) + l, color: color})
		}
	}

	t, _ := task.Load(config.TasksDir(root), id)
	st, _ := state.Get(root, id)
	ls = append(ls, lineaSticker{texto: id + "  " + t.Titulo, color: rgbPresion}, lineaSticker{})
	campo("estado", st.Estado, rgbTinta)
	campo("porque", t.Porque, rgbTinta)
	campo("listo_cuando", t.ListoCuando, rgbTinta)
	campo("tocar_solo", strings.Join(t.TocarSolo, ", "), rgbTinta)
	campo("depende_de", strings.Join(t.DependeDe, ", "), rgbTinta)
	campo("peso", t.Peso, rgbTinta)
	campo("pregunta", st.Pregunta, rgbAlerta)

	intentos, _ := loop.ReadAttempts(root, id)
	ls = append(ls, lineaSticker{}, lineaSticker{texto: idioma.T("INTENTOS · ") + strconv.Itoa(len(intentos)), color: rgbPresion})
	if len(intentos) == 0 {
		ls = append(ls, lineaSticker{texto: idioma.T("  la tarea todavía no corre"), color: rgbApagado})
	}
	for _, a := range intentos {
		salida, color := "—", rgbAlerta
		if a.SalidaCodigo != nil {
			salida = strconv.Itoa(*a.SalidaCodigo)
			if *a.SalidaCodigo == 0 && (a.Revision == nil || a.Revision.Aprobada) {
				color = rgbPresion
			}
		}
		ls = append(ls, lineaSticker{texto: "  " + strconv.Itoa(a.Intento) + idioma.T(" · salida ") + salida +
			" · +" + strconv.Itoa(a.LineasMas) + "/-" + strconv.Itoa(a.LineasMenos) +
			" · " + a.Modelo + " · " + strconv.Itoa(a.Tokens.Entrada) + "↑/" + strconv.Itoa(a.Tokens.Salida) + "↓ tokens", color: color})
		campo(idioma.T("    archivos"), strings.Join(a.ArchivosTocados, ", "), rgbApagado)
		campo(idioma.T("    revertido"), strings.Join(a.RevertidosFueraDeAlcance, ", "), rgbAlerta)
		campo("    agente", a.ErrorAgente, rgbAlerta)
		if a.Revision != nil {
			campo("    revisor", a.Revision.Cambios, rgbEspera)
			campo("    visual", a.Revision.Visual, rgbEspera)
		}
		campo("    log", a.Log, rgbApagado)
	}
	return ls
}

// envolver parte s en líneas de hasta ancho columnas, por palabras; una
// palabra más larga que el ancho queda entera en su línea.
func envolver(s string, ancho int) []string {
	var out []string
	linea := ""
	for _, p := range strings.Fields(s) {
		if linea != "" && len([]rune(linea))+1+len([]rune(p)) > ancho {
			out = append(out, linea)
			linea = ""
		}
		if linea != "" {
			linea += " "
		}
		linea += p
	}
	return append(out, linea)
}
