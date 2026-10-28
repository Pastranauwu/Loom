package ship

import (
	"path"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/task"
)

var (
	tsPermisivo   = regexp.MustCompile(`@ts-(ignore|nocheck)|\bas any\b|:\s*any\b|<any>`)
	tsAnyFirma    = regexp.MustCompile(`:\s*any\b|<\s*any\s*[,>]|,\s*any\s*>|\bany\s*\[\]|\|\s*any\b|\bany\s*\||=>\s*any\b`)
	rustUnsafe    = regexp.MustCompile(`\bunsafe\b`)
	pyDef         = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	tsExtensiones = map[string]bool{".ts": true, ".tsx": true, ".mts": true, ".cts": true}
)

// aplicaEstricto reports whether the diff touches a tier-1 file the step
// reads: TypeScript, Rust or non-test Python.
func aplicaEstricto(archivos []string) bool {
	for _, a := range archivos {
		if lenguajeEstricto(a) != "" {
			return true
		}
	}
	return false
}

func lenguajeEstricto(archivo string) string {
	ext := strings.ToLower(path.Ext(archivo))
	switch {
	case tsExtensiones[ext]:
		return "ts"
	case ext == ".rs":
		return "rust"
	case ext == ".py" && !task.EsArchivoDePrueba(archivo):
		return "python"
	}
	return ""
}

// escanearEstricto looks in the added lines for what lets code through the
// compiler: ts-ignore and any in TypeScript, unsafe in Rust without the
// contract saying why, Python functions without full type hints.
func escanearEstricto(diff string, t task.Task) []Hallazgo {
	var h []Hallazgo
	justificado := strings.Contains(strings.ToLower(t.Riesgos+" "+t.Notas+" "+strings.Join(t.Expone, " ")), "unsafe")
	ts := false
	for _, ad := range parseDiffAnadido(diff) {
		switch lenguajeEstricto(ad.nombre) {
		case "ts":
			ts = true
			for _, l := range ad.lineas {
				if tsPermisivo.MatchString(l) {
					h = append(h, Hallazgo{Tipo: idioma.T("TypeScript permisivo"), Archivo: ad.nombre, Detalle: recortar(l)})
				}
			}
		case "rust":
			for _, l := range ad.lineas {
				if !justificado && rustUnsafe.MatchString(l) && !esLineaDeComentario(l) {
					h = append(h, Hallazgo{Tipo: idioma.T("unsafe sin justificar en el contrato"), Archivo: ad.nombre, Detalle: recortar(l)})
				}
			}
		case "python":
			for _, l := range ad.lineas {
				if falta := sinTipos(l); falta != "" {
					h = append(h, Hallazgo{Tipo: idioma.T("Python sin tipos: ") + falta, Archivo: ad.nombre, Detalle: recortar(l)})
				}
			}
		}
	}
	// lo que otras tareas importan sin leer el cuerpo: un any ahí apaga el
	// compilador en cada consumidor
	if ts {
		for _, e := range t.Expone {
			if tsAnyFirma.MatchString(e) {
				h = append(h, Hallazgo{Tipo: idioma.T("contrato TypeScript con any"), Archivo: "expone", Detalle: recortar(e)})
			}
		}
	}
	return h
}

// sinTipos names what a one-line def lacks ("" = nothing). A def that
// continues on the next line is skipped: the step only judges what it can
// read whole, and mypy --strict in the suite covers the rest.
func sinTipos(linea string) string {
	m := pyDef.FindStringSubmatchIndex(linea)
	if m == nil {
		return ""
	}
	nombre := linea[m[2]:m[3]]
	resto := linea[m[1]:]
	prof, cierre := 1, -1
	for i, c := range resto {
		switch c {
		case '(', '[', '{':
			prof++
		case ')', ']', '}':
			prof--
		}
		if prof == 0 {
			cierre = i
			break
		}
	}
	if cierre < 0 {
		return ""
	}
	if !strings.Contains(resto[cierre+1:], "->") && nombre != "__init__" {
		return idioma.T("sin tipo de retorno")
	}
	for _, arg := range argsDePrimerNivel(resto[:cierre]) {
		arg = strings.TrimLeft(strings.TrimSpace(arg), "*")
		if arg == "" || arg == "self" || arg == "cls" || arg == "/" {
			continue
		}
		nombreArg, _, _ := strings.Cut(arg, "=")
		if !strings.Contains(nombreArg, ":") {
			return idioma.T("argumento sin tipo")
		}
	}
	return ""
}

func argsDePrimerNivel(s string) []string {
	var out []string
	prof, ini := 0, 0
	for i, c := range s {
		switch c {
		case '(', '[', '{':
			prof++
		case ')', ']', '}':
			prof--
		case ',':
			if prof == 0 {
				out = append(out, s[ini:i])
				ini = i + 1
			}
		}
	}
	return append(out, s[ini:])
}
