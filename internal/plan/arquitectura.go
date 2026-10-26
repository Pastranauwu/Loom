package plan

import (
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

// Marcas con que Parse y spec.Apply componen las notas de un contrato:
// reglas de la spec, arquitectura del plan y lo propio de la tarea. El
// bucle las vuelve a separar para armar el prompt con lo común primero.
const (
	MarcaReglas         = "Reglas de la especificación:\n"
	MarcaArquitectura   = "Arquitectura del plan:\n"
	MarcaImplementacion = "Implementación de esta tarea:\n"

	// encabezados de las secciones que se recortan por tarea
	SeccionArbol  = "ÁRBOL DE ARCHIVOS"
	SeccionFirmas = "FIRMAS PÚBLICAS"
)

// Notas son las notas de un contrato separadas por origen.
type Notas struct {
	Reglas       string // bloque de reglas con su marca, o ""
	Arquitectura string // texto de la arquitectura, sin marca, o ""
	Tarea        string // implementación de la tarea y cualquier otra nota
}

// SepararNotas parte las notas de un contrato. Sin marcas todo queda como
// nota de la tarea: un contrato escrito a mano no pierde nada.
func SepararNotas(notas string) Notas {
	var n Notas
	resto := notas
	if strings.HasPrefix(resto, MarcaReglas) {
		fin := strings.Index(resto, "\n\n")
		if fin == -1 {
			return Notas{Reglas: resto}
		}
		n.Reglas, resto = resto[:fin], resto[fin+2:]
	}
	if strings.HasPrefix(resto, MarcaArquitectura) {
		if fin := strings.Index(resto, "\n\n"+MarcaImplementacion); fin != -1 {
			n.Arquitectura = resto[len(MarcaArquitectura):fin]
			resto = resto[fin+2+len(MarcaImplementacion):]
		}
	}
	n.Tarea = resto
	return n
}

// Arquitectura es la arquitectura del plan dividida para el prompt.
type Arquitectura struct {
	Comun  string // idéntica para todas las tareas del plan
	Arbol  string // ÁRBOL DE ARCHIVOS, solo lo que la tarea toca o usa
	Firmas string // FIRMAS PÚBLICAS, solo los bloques que la tarea toca o usa
}

// RecortarArquitectura deja en Comun todas las secciones salvo el árbol y
// las firmas, que se recortan a lo que t declara en tocar_solo, usa y
// expone. Las secciones se reconocen por párrafo y encabezado; si el plan
// no los trae, la arquitectura queda entera en Comun.
func RecortarArquitectura(arq string, t task.Task) Arquitectura {
	var comun []string
	var a Arquitectura
	var rutasFirmas []string // dueñas de lo que t usa, para el árbol
	parrafos := strings.Split(arq, "\n\n")
	for _, p := range parrafos {
		if strings.HasPrefix(p, SeccionFirmas) {
			a.Firmas, rutasFirmas = recortarFirmas(p, t)
		}
	}
	for _, p := range parrafos {
		switch {
		case strings.HasPrefix(p, SeccionFirmas):
		case strings.HasPrefix(p, SeccionArbol):
			a.Arbol = recortarArbol(p, append(append([]string(nil), t.TocarSolo...), rutasFirmas...))
		default:
			comun = append(comun, p)
		}
	}
	a.Comun = strings.Join(comun, "\n\n")
	return a
}

// bloqueFirma es la cabecera "[T-007 src/render/pixel.js] ..." de las
// firmas. El id es el del planificador, que puede no coincidir con el
// asignado: se identifica por ruta, nunca por id.
var bloqueFirma = regexp.MustCompile(`^\[\S+ ([^\]]+)\]`)

// recortarFirmas conserva los bloques cuya ruta cae en tocar_solo o que
// nombran algo de usa o expone. Un bloque va entero con sus líneas de
// continuación: ahí viven tipos como `Store = {...}` que la firma necesita.
func recortarFirmas(seccion string, t task.Task) (string, []string) {
	var nombres []string
	for _, f := range append(append([]string(nil), t.Usa...), t.Expone...) {
		if n := ultimoSegmento(task.NombreDeFirma(f)); n != "" {
			nombres = append(nombres, n)
		}
	}
	lineas := strings.Split(seccion, "\n")
	out := []string{lineas[0]}
	var rutas []string
	var bloque []string
	cerrar := func() {
		if len(bloque) == 0 {
			return
		}
		m := bloqueFirma.FindStringSubmatch(bloque[0])
		ruta := strings.TrimSpace(m[1])
		texto := strings.Join(bloque, "\n")
		propio := config.MatchesAny(t.TocarSolo, ruta)
		usado := !propio && nombraAlguno(texto, nombres)
		if propio || usado {
			out = append(out, bloque...)
		}
		if usado {
			rutas = append(rutas, ruta)
		}
		bloque = nil
	}
	for _, l := range lineas[1:] {
		if bloqueFirma.MatchString(l) {
			cerrar()
			bloque = []string{l}
		} else if len(bloque) > 0 {
			bloque = append(bloque, l)
		} else {
			out = append(out, l) // texto suelto antes del primer bloque
		}
	}
	cerrar()
	return strings.Join(out, "\n"), rutas
}

// recortarArbol conserva las líneas cuyas rutas caen en rutas, y las que
// no nombran ninguna ruta: esas son convenciones del árbol entero.
func recortarArbol(seccion string, rutas []string) string {
	lineas := strings.Split(seccion, "\n")
	out := []string{lineas[0]}
	for _, l := range lineas[1:] {
		archivos := rutasDeLinea(l)
		if len(archivos) == 0 {
			out = append(out, l)
			continue
		}
		for _, f := range archivos {
			if config.MatchesAny(rutas, f) {
				out = append(out, l)
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// rutasDeLinea saca las rutas del principio de una línea del árbol:
// " src/a.js (T-001) desc" o " index.html, styles.css (T-012)".
func rutasDeLinea(l string) []string {
	cabeza := strings.TrimSpace(l)
	if i := strings.IndexAny(cabeza, "(\t"); i != -1 {
		cabeza = cabeza[:i]
	}
	if i := strings.Index(cabeza, "  "); i != -1 {
		cabeza = cabeza[:i]
	}
	var rutas []string
	for _, c := range strings.Split(cabeza, ",") {
		c = strings.TrimSpace(c)
		if c == "" || strings.ContainsAny(c, " ") || !strings.ContainsAny(c, "./") {
			return nil
		}
		rutas = append(rutas, c)
	}
	return rutas
}

// nombraAlguno reporta si texto contiene alguno de los nombres como
// palabra completa.
func nombraAlguno(texto string, nombres []string) bool {
	for _, n := range nombres {
		if regexp.MustCompile(`(^|[^\w])` + regexp.QuoteMeta(n) + `([^\w]|$)`).MatchString(texto) {
			return true
		}
	}
	return false
}

// ultimoSegmento deja "Send" de "wol.Send" y "handleKey" de
// "App.handleKey": en la arquitectura las firmas aparecen sin paquete.
// También corta tipos pegados sin paréntesis ("LEVELS: readonly Level[]").
func ultimoSegmento(nombre string) string {
	if i := strings.IndexAny(nombre, " :=<"); i != -1 {
		nombre = nombre[:i]
	}
	if i := strings.LastIndex(nombre, "."); i != -1 {
		return nombre[i+1:]
	}
	return nombre
}
