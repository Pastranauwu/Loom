package ship

import (
	"strings"
)

// Hallazgo es algo que un escáner marcó en el diff.
type Hallazgo struct {
	Tipo    string `json:"tipo"`
	Archivo string `json:"archivo,omitempty"`
	Detalle string `json:"detalle,omitempty"`
	linea   string // la línea entera, para buscarla en el estado final
}

// archivoDiff es un archivo del diff con sus líneas añadidas.
type archivoDiff struct {
	nombre string
	lineas []string
}

// parseDiffAnadido recorre un unified diff y devuelve, por archivo, las
// líneas añadidas (sin el +). Los escáneres miran solo lo que entra al
// PR, no lo que se quita.
func parseDiffAnadido(diff string) []archivoDiff {
	var out []archivoDiff
	var cur *archivoDiff
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			nombre := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			nombre = strings.TrimPrefix(nombre, "b/")
			out = append(out, archivoDiff{nombre: nombre})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			cur.lineas = append(cur.lineas, strings.TrimPrefix(line, "+"))
			continue
		}
		// hunk nuevo o línea de contexto: lo añadido a ambos lados no es
		// contiguo, y una línea vacía impide que se lea como un solo bloque
		if (strings.HasPrefix(line, "@@") || strings.HasPrefix(line, " ")) &&
			len(cur.lineas) > 0 && cur.lineas[len(cur.lineas)-1] != "" {
			cur.lineas = append(cur.lineas, "")
		}
	}
	return out
}

// resumenHallazgos reduce una lista de hallazgos a un motivo legible.
func resumenHallazgos(h []Hallazgo) string {
	if len(h) == 0 {
		return ""
	}
	var b strings.Builder
	for i, hallazgo := range h {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(hallazgo.Tipo)
		if hallazgo.Archivo != "" {
			b.WriteString(" en " + hallazgo.Archivo)
		}
		if hallazgo.Detalle != "" {
			b.WriteString(" (" + hallazgo.Detalle + ")")
		}
	}
	return b.String()
}
