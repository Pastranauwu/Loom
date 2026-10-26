package examiner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pastranauwu/loom/internal/esqueleto"
	"github.com/Pastranauwu/loom/internal/task"
)

// contratoDelEsqueleto entrega al examinador solo ejemplos y firmas de
// stubs. Nunca manda cuerpos del archivo, que pueden contener código previo.
func contratoDelEsqueleto(roomPath string, t task.Task) (string, error) {
	var partes []string
	for _, rel := range t.TocarSolo {
		if strings.ContainsAny(rel, "*?[") || task.EsArchivoDePrueba(rel) {
			continue
		}
		abs := filepath.Join(roomPath, rel)
		b, err := os.ReadFile(abs)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", rel, err)
		}
		src := string(b)
		if !strings.Contains(src, esqueleto.Marca) {
			continue
		}
		for _, c := range casosYFirma(src) {
			partes = append(partes, rel+"\n"+c)
		}
	}
	return strings.Join(partes, "\n\n"), nil
}

// casosYFirma saca, por cada "Casos:" del archivo, sus ejemplos y la
// firma del stub al que pertenecen. Lee por líneas y según el tipo de
// comentario en que están los casos:
//
//   - de línea (// o #): los casos son el resto de esa línea y las líneas
//     de comentario que siguen; la firma viene después.
//   - de bloque (/* … */): hasta la línea que cierra el bloque; la firma
//     viene después.
//   - docstring de Python: hasta las comillas que lo cierran; la firma es
//     el def de arriba.
//
// Antes buscaba el próximo "*/" de todo el archivo: con comentarios de
// línea se llevaba código de por medio (y otra firma) hasta el siguiente
// bloque, y sin ninguno no encontraba nada (Go con casos en la misma
// línea, Python). Nunca devuelve líneas de cuerpo.
func casosYFirma(src string) []string {
	lineas := strings.Split(src, "\n")
	var out []string
	for i := 0; i < len(lineas); i++ {
		_, resto, hay := strings.Cut(lineas[i], "Casos:")
		if !hay {
			continue
		}
		casos := []string{}
		agregar := func(l string) {
			if l = limpiarComentario(l); l != "" && len(casos) < 10 {
				casos = append(casos, l)
			}
		}
		t := strings.TrimSpace(lineas[i])
		var firma string
		fin := i
		switch {
		case enDocstring(lineas, i):
			agregar(resto)
			for fin = i + 1; fin < len(lineas) && !tieneComillasTriples(lineas[fin]); fin++ {
				agregar(lineas[fin])
			}
			firma = firmaDeArriba(lineas, i)
		case strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#"):
			prefijo := "//"
			if strings.HasPrefix(t, "#") {
				prefijo = "#"
			}
			agregar(resto)
			for fin = i + 1; fin < len(lineas) && strings.HasPrefix(strings.TrimSpace(lineas[fin]), prefijo); fin++ {
				agregar(lineas[fin])
			}
			firma = firmaDeAbajo(lineas, fin)
		default:
			if cierre := strings.Index(resto, "*/"); cierre >= 0 {
				agregar(resto[:cierre])
			} else {
				agregar(resto)
				for fin = i + 1; fin < len(lineas); fin++ {
					antes, _, cierra := strings.Cut(lineas[fin], "*/")
					agregar(antes)
					if cierra {
						break
					}
				}
			}
			firma = firmaDeAbajo(lineas, fin+1)
		}
		if len(casos) > 0 && firma != "" {
			if c := claseQueContiene(lineas, i); c != "" && !strings.Contains(firma, "class ") {
				firma = c + " · " + firma
			}
			out = append(out, "Firma: "+firma+"\nCasos: "+strings.Join(casos, "; "))
		}
		i = fin
	}
	return out
}

// limpiarComentario quita de una línea los marcadores de comentario y la
// viñeta, y descarta las etiquetas de otra sección del contrato.
func limpiarComentario(l string) string {
	l = strings.TrimSpace(l)
	for _, p := range []string{"//", "#", "*", "-"} {
		l = strings.TrimSpace(strings.TrimPrefix(l, p))
	}
	return l
}

func tieneComillasTriples(l string) bool {
	return strings.Contains(l, `"""`) || strings.Contains(l, "'''")
}

// enDocstring reporta si la línea i cae dentro de un docstring: hay un
// número impar de comillas triples antes de ella.
func enDocstring(lineas []string, i int) bool {
	n := 0
	for _, l := range lineas[:i] {
		n += strings.Count(l, `"""`) + strings.Count(l, "'''")
	}
	return n%2 == 1
}

// firmaDeAbajo junta la declaración que sigue al comentario, hasta la
// llave que abre el cuerpo (sin incluirlo).
func firmaDeAbajo(lineas []string, desde int) string {
	var partes []string
	for _, raw := range lineas[min(desde, len(lineas)):] {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "@") {
			continue
		}
		if j := strings.Index(l, "{"); j >= 0 {
			partes = append(partes, strings.TrimSpace(l[:j]))
			break
		}
		partes = append(partes, l)
		if strings.HasSuffix(l, ":") || strings.HasSuffix(l, ";") || len(partes) == 8 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(partes, " "))
}

// firmaDeArriba es el def (o class) de Python dueño del docstring.
func firmaDeArriba(lineas []string, desde int) string {
	for i := desde; i >= 0; i-- {
		l := strings.TrimSpace(lineas[i])
		if strings.HasPrefix(l, "def ") || strings.HasPrefix(l, "async def ") || strings.HasPrefix(l, "class ") {
			return strings.TrimSuffix(l, ":")
		}
	}
	return ""
}

// claseQueContiene nombra la clase de un método: sin ella el examinador
// ve "total(items): number" y no sabe de qué objeto es.
func claseQueContiene(lineas []string, desde int) string {
	sangria := func(l string) int { return len(l) - len(strings.TrimLeft(l, " \t")) }
	propia := sangria(lineas[desde])
	for i := desde - 1; i >= 0; i-- {
		l := lineas[i]
		if strings.TrimSpace(l) == "" || sangria(l) >= propia {
			continue
		}
		for _, campo := range strings.Fields(l) {
			if campo == "class" {
				_, nombre, _ := strings.Cut(l, "class ")
				nombre = strings.FieldsFunc(nombre, func(r rune) bool { return r == ' ' || r == '{' || r == '(' || r == ':' || r == '<' })[0]
				return "class " + nombre
			}
		}
		propia = sangria(l)
	}
	return ""
}
