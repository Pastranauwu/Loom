package ship

import (
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/task"
	"path"
	"regexp"
	"strings"
)

// escanearRuido busca la basura que no debe llegar al PR: prints de debug, código
// comentado y archivos temporales. Conservador a propósito: un falso
// positivo frena un PR válido.
func escanearRuido(diff string, archivos []string) []Hallazgo {
	var h []Hallazgo
	for _, ad := range parseDiffAnadido(diff) {
		// la documentacion muestra codigo a proposito: un README que
		// explica como depurar un cliente JS lleva console.log en un
		// bloque, y marcarlo frenaba el ship de un PR correcto
		if esDocumentacion(ad.nombre) {
			continue
		}
		entrada := esPuntoDeEntrada(ad.nombre) || declaraEntrada(ad.lineas)
		for _, linea := range ad.lineas {
			if t := tipoDebug(linea, entrada); t != "" {
				h = append(h, Hallazgo{Tipo: t, Archivo: ad.nombre, Detalle: recortar(linea), linea: linea})
			} else if !task.EsArchivoDePrueba(ad.nombre) && (idioma.Tiene(linea, idioma.Idea) || idioma.Tiene(linea, idioma.Casos)) {
				// el contrato del stub era una instrucción para el agente;
				// en el código entregado se pudre (100 líneas de
				// contrato sobre una función de 130, ya contradiciéndola)
				h = append(h, Hallazgo{Tipo: "contrato del esqueleto", Archivo: ad.nombre, Detalle: recortar(linea), linea: linea})
			} else if !esDirectiva(linea) && marcaHumana.MatchString(linea) {
				h = append(h, Hallazgo{Tipo: "comentario humano", Archivo: ad.nombre, Detalle: recortar(linea), linea: linea})
			} else if esCodigoComentado(linea) {
				h = append(h, Hallazgo{Tipo: "código comentado", Archivo: ad.nombre, Detalle: recortar(linea), linea: linea})
			}
		}
		h = append(h, bloquesNarrativos(ad)...)
	}
	for _, a := range archivos {
		if esTemporal(a) {
			h = append(h, Hallazgo{Tipo: "archivo temporal", Archivo: a})
		}
	}
	return h
}

var patronesDebug = []struct {
	tipo string
	re   *regexp.Regexp
	// esSalida marca los patrones que en el punto de entrada de un
	// programa NO son ruido, sino lo que el programa hace. Un CLI cuyo
	// trabajo es imprimir un resultado no puede entregarse nunca si
	// `fmt.Println` en su main.go cuenta como print de debug.
	esSalida bool
}{
	// log.Print* queda afuera a propósito: es el logger estándar de Go
	// para producción (servicios de red, CLIs de larga vida), no un
	// rastro de debug como fmt.Println. Marcarlo como ruido frena
	// cualquier código que loguee de verdad.
	{"print de debug", regexp.MustCompile(`fmt\.Print(?:ln|f)?\(`), true},
	{"print de debug", regexp.MustCompile(`\bprintln\(`), true},
	{"print de debug", regexp.MustCompile(`\bprint\(`), true},
	// console.error y console.warn quedan afuera por la misma razón que
	// log.Print: reportan errores de verdad (el catch que avisa por qué
	// falló el arranque), no son rastro de depuración.
	{"print de debug", regexp.MustCompile(`console\.(?:log|info)\(`), true},
	{"print de debug", regexp.MustCompile(`System\.out\.print(?:ln)?\(`), true},
	// estos no son salida de nadie: son depuradores y volcados, ruido
	// en cualquier archivo
	{"print de debug", regexp.MustCompile(`console\.debug\(`), false},
	{"print de debug", regexp.MustCompile(`\bdebugger\b`), false},
	{"print de debug", regexp.MustCompile(`pdb\.set_trace\(`), false},
	{"print de debug", regexp.MustCompile(`\bbreakpoint\(`), false},
	{"print de debug", regexp.MustCompile(`var_dump\(`), false},
	{"print de debug", regexp.MustCompile(`\bprint_r\(`), false},
	{"print de debug", regexp.MustCompile(`\bdd\(`), false},
}

func tipoDebug(linea string, puntoDeEntrada bool) string {
	for _, p := range patronesDebug {
		if p.esSalida && puntoDeEntrada {
			continue
		}
		if p.re.MatchString(sinCadenas(linea)) {
			return p.tipo
		}
	}
	return ""
}

// esDocumentacion reporta si el archivo existe para ser leído, no
// ejecutado. Ahí un print no es un descuido: es el ejemplo.
func esDocumentacion(archivo string) bool {
	limpio := strings.TrimPrefix(path.Clean(archivo), "./")
	switch strings.ToLower(path.Ext(limpio)) {
	case ".md", ".mdx", ".markdown", ".rst", ".txt", ".adoc", ".asciidoc", ".org":
		return true
	}
	for _, dir := range []string{"docs/", "doc/", "documentation/", "ejemplos/", "examples/"} {
		if strings.HasPrefix(limpio, dir) || strings.Contains(limpio, "/"+dir) {
			return true
		}
	}
	return false
}

// esPuntoDeEntrada reporta si el archivo es por dónde arranca un
// programa, donde escribir a stdout es la función y no un descuido.
// Solo por convención de ruta y nombre: leer el archivo para confirmar
// que declara `package main` obligaría a tener el cuarto a mano, y el
// escáner trabaja sobre el diff.
func esPuntoDeEntrada(archivo string) bool {
	limpio := strings.TrimPrefix(path.Clean(archivo), "./")
	for _, dir := range []string{"cmd/", "bin/", "cli/", "scripts/"} {
		if strings.HasPrefix(limpio, dir) || strings.Contains(limpio, "/"+dir) {
			return true
		}
	}
	switch path.Base(limpio) {
	case "main.go", "main.py", "__main__.py", "main.rs", "cli.py", "cli.js", "cli.ts", "main.js", "main.ts":
		return true
	}
	return false
}

// marcaEntrada reconoce la línea con que un archivo se declara ejecutable:
// el guard de Python, la función main de Go, Rust o Java, o un shebang.
var marcaEntrada = regexp.MustCompile(`^(?:if __name__ == ['"]__main__['"]|func main\(|fn main\(|#!/)|\bstatic void main\(`)

// declaraEntrada reporta si lo añadido a un archivo lo declara punto de
// entrada. Cubre el script que no vive en una ruta de esPuntoDeEntrada.
// Solo ve el diff: si el main ya estaba y la tarea no lo tocó, no lo
// reconoce y sigue mandando la ruta.
func declaraEntrada(lineas []string) bool {
	for _, l := range lineas {
		if marcaEntrada.MatchString(l) {
			return true
		}
	}
	return false
}

// esCodigoComentado marca una línea de comentario que en realidad es
// código: termina en ; o empieza con una palabra clave tras el marcador.
func esCodigoComentado(linea string) bool {
	t := strings.TrimSpace(linea)
	if !esComentario(t) {
		return false
	}
	sin := strings.TrimLeft(t, "/#*")
	sin = strings.TrimSpace(sin)
	if sin == "" {
		return false
	}
	// ";" en medio de una oración es narrativa normal ("Defaults to X;
	// overridable in tests."); código comentado real termina la línea
	// en el punto y coma, sin nada más después. Tampoco alcanza solo:
	// una oración partida en dos líneas también cierra en ";" ("las
	// suites por pieza (engine.test.js, etc.);"). Tiene que verse como
	// código: una llamada pegada, una asignación o un return.
	if strings.HasSuffix(sin, ";") && formaDeSentencia.MatchString(sin) {
		return true
	}
	// palabra clave al inicio no alcanza: "for", "if", "type" también
	// arrancan oraciones normales ("for Echo discovery, ..."). Solo
	// cuenta si además tiene pinta de código (paréntesis, ":=" o llave).
	tieneFormaDeCodigo := strings.ContainsAny(sin, "({") || strings.Contains(sin, ":=")
	if tieneFormaDeCodigo {
		for _, kw := range []string{"return ", "if ", "for ", "while ", "func ", "def ", "var ", "let ", "const ", "import ", "class ", "type "} {
			if strings.HasPrefix(sin, kw) {
				return true
			}
		}
	}
	return false
}

// cadena es un literal entre comillas simples, dobles o invertidas.
var cadena = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|'(?:[^'\\\\]|\\\\.)*'|`[^`]*`")

// sinCadenas vacía los literales: assertNotIn("debugger", js) nombra el
// depurador, no lo llama.
func sinCadenas(linea string) string {
	return cadena.ReplaceAllString(linea, `""`)
}

// formaDeSentencia es lo que una sentencia tiene y la prosa no: una
// llamada con el paréntesis pegado al nombre, una asignación o un return.
var formaDeSentencia = regexp.MustCompile(`\w\(|[^=!<>]=[^=>]|^return\b`)

// marcaHumana son los marcadores que un comentario deja para una persona:
// el código se entrega a agentes, que no los leen ni los resuelven.
var marcaHumana = regexp.MustCompile(`(?:^\s*\*|//|#|/\*|--).*(?:\b(?:TODO|FIXME|HACK|XXX|NOTE)\b|(?i:do not touch|no tocar))`)

// directivasDeComentario son comentarios que una herramienta lee: quitarlos
// cambia el comportamiento, así que nunca son ruido.
var directivasDeComentario = []string{
	"//go:", "//nolint", "// nolint", "// +build", "//export", "//line ", "#!", "# -*-",
	"eslint-", "@ts-", "noqa", "type: ignore", "pylint:", "istanbul ", "prettier-ignore", "#region", "#endregion",
}

func esDirectiva(linea string) bool {
	t := strings.TrimSpace(linea)
	for _, d := range directivasDeComentario {
		if strings.Contains(t, d) {
			return true
		}
	}
	return false
}

// esLineaDeComentario es más estricta que esComentario: una línea que solo
// es comentario, sin confundir `*p = 1` ni `#[derive]` con prosa.
func esLineaDeComentario(linea string) bool {
	t := strings.TrimSpace(linea)
	switch {
	case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "/*"), strings.HasPrefix(t, "-- "):
		return true
	case strings.HasPrefix(t, "#"):
		return !strings.HasPrefix(t, "#[") && !strings.HasPrefix(t, "#!") && !strings.HasPrefix(t, "#include") && !strings.HasPrefix(t, "#define")
	case t == "*", strings.HasPrefix(t, "* "):
		return true
	}
	return false
}

// bloquesNarrativos devuelve un hallazgo por cada racha de más de dos
// líneas de comentario seguidas. Las rachas con directivas y el contrato
// del esqueleto (Idea:/Casos:, que ya tiene su aviso) quedan fuera.
// parseDiffAnadido corta las rachas en cada hunk y línea de contexto.
func bloquesNarrativos(ad archivoDiff) []Hallazgo {
	var h []Hallazgo
	var racha []string
	cerrar := func() {
		if len(racha) > 2 && !rachaExenta(racha) {
			h = append(h, Hallazgo{Tipo: "comentario narrativo", Archivo: ad.nombre, Detalle: recortar(racha[0]), linea: racha[0]})
		}
		racha = nil
	}
	for _, l := range ad.lineas {
		if esLineaDeComentario(l) {
			racha = append(racha, l)
		} else {
			cerrar()
		}
	}
	cerrar()
	return h
}

func rachaExenta(racha []string) bool {
	for _, l := range racha {
		if esDirectiva(l) || idioma.Tiene(l, idioma.Idea) || idioma.Tiene(l, idioma.Casos) {
			return true
		}
	}
	return false
}

func esComentario(s string) bool {
	// "--" sin espacio es una variable CSS (--sombra-1: 0 1px;) o una
	// bandera, no un comentario SQL/Lua
	if strings.HasPrefix(s, "--") && !strings.HasPrefix(s, "-- ") && s != "--" {
		return false
	}
	for _, m := range []string{"//", "#", "/*", "*", "--", "<!--"} {
		if strings.HasPrefix(s, m) {
			return true
		}
	}
	return false
}

func esTemporal(nombre string) bool {
	base := path.Base(nombre)
	for _, suf := range []string{".tmp", ".bak", ".swp", ".orig", ".rej", "~"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.HasPrefix(base, "~$") {
		return true
	}
	return base == ".DS_Store" || base == "Thumbs.db"
}

func recortar(s string) string {
	s = strings.TrimSpace(s)
	const max = 80
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
