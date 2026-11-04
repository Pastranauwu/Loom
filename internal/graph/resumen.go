package graph

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxArchivos = 20
	maxBytes    = 6000
)

// Resumen es el mapa compacto que lee el planificador en vez de explorar el
// repo: los archivos más cercanos a la petición (o los más importados, si no
// hay coincidencias) con sus símbolos y dependencias. Determinista: sin
// fechas ni orden de mapa. Vacío si el grafo no tiene archivos.
func Resumen(g *Graph, consulta string) string {
	if g == nil || len(g.Files) == 0 {
		return ""
	}
	elegidos, titulo := ranking(g, consulta)
	var b strings.Builder
	fmt.Fprintf(&b, "Mapa del repo (archivos %s: propósito, símbolos públicos y dependencias). Parte de aquí, no explores a ciegas:\n", titulo)
	for i, f := range elegidos {
		if i >= maxArchivos {
			break
		}
		n := g.Files[f]
		linea := f
		if n.Doc != "" {
			linea += " (" + n.Doc + ")"
		}
		linea += ": " + recorta(n.Symbols, 12)
		if len(n.Imports) > 0 {
			linea += " | importa: " + recorta(n.Imports, 6)
		}
		if len(n.ImportedBy) > 0 {
			linea += " | usado por: " + recorta(n.ImportedBy, 6)
		}
		if b.Len()+len(linea) > maxBytes {
			break
		}
		b.WriteString(linea + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Relevantes son los archivos que más coinciden con la consulta, de mayor a
// menor; vacío si ninguno coincide (los más importados no cuentan).
func Relevantes(g *Graph, consulta string) []string {
	if g == nil {
		return nil
	}
	if fs, titulo := ranking(g, consulta); titulo == tituloCoinciden {
		return fs
	}
	return nil
}

const tituloCoinciden = "relevantes a la petición"

// ranking ordena los archivos por coincidencia con la consulta o, si
// ninguno coincide, por cuántos los importan. Determinista.
func ranking(g *Graph, consulta string) ([]string, string) {
	palabras := []string{}
	for _, w := range strings.FieldsFunc(strings.ToLower(consulta), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}) {
		if len(w) >= 4 {
			palabras = append(palabras, w)
		}
	}
	type punto struct {
		f string
		n int
	}
	var coinciden, hubs []punto
	for f, n := range g.Files {
		score := 0
		lf := strings.ToLower(f)
		for _, w := range palabras {
			if strings.Contains(lf, w) {
				score += 3
			}
			if strings.Contains(strings.ToLower(n.Doc), w) {
				score += 2
			}
			for _, s := range n.Symbols {
				if strings.Contains(strings.ToLower(s), w) {
					score += 2
				}
			}
		}
		if score > 0 {
			coinciden = append(coinciden, punto{f, score})
		}
		hubs = append(hubs, punto{f, len(n.ImportedBy)})
	}
	elegidos, titulo := coinciden, tituloCoinciden
	if len(elegidos) == 0 {
		elegidos, titulo = hubs, "más usados del repo"
	}
	sort.Slice(elegidos, func(i, j int) bool {
		if elegidos[i].n != elegidos[j].n {
			return elegidos[i].n > elegidos[j].n
		}
		return elegidos[i].f < elegidos[j].f
	})
	out := make([]string, len(elegidos))
	for i, p := range elegidos {
		out[i] = p.f
	}
	return out, titulo
}

func recorta(s []string, max int) string {
	if len(s) == 0 {
		return "-"
	}
	if len(s) > max {
		return strings.Join(s[:max], ", ") + fmt.Sprintf(" (+%d)", len(s)-max)
	}
	return strings.Join(s, ", ")
}
