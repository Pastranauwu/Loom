package topo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Estilo colors the text view; the zero value prints plain text.
type Estilo struct {
	Titulo, Apagado, Ok, Mal, Espera func(string) string
}

func (e Estilo) f(fn func(string) string, s string) string {
	if fn == nil {
		return s
	}
	return fn(s)
}

func glifo(estado string) string {
	switch estado {
	case "lista":
		return "✓"
	case "en_curso":
		return "◐"
	case "detenida":
		return "✗"
	}
	return "·"
}

func (e Estilo) glifo(estado string) string {
	g := glifo(estado)
	switch estado {
	case "lista":
		return e.f(e.Ok, g)
	case "detenida":
		return e.f(e.Mal, g)
	case "en_curso":
		return e.f(e.Espera, g)
	}
	return e.f(e.Apagado, g)
}

// Texto draws the map as a tree: DNA and gates on top, then each module
// with its tasks, contracts and neighbours.
func Texto(m Mapa, e Estilo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", e.f(e.Titulo, fmt.Sprintf("loom map · %d módulos · %d aristas", len(m.Modulos), len(m.Aristas))))

	b.WriteString(e.f(e.Titulo, "DNA") + "\n")
	if len(m.DNA) == 0 {
		b.WriteString("  " + e.f(e.Apagado, "ninguno · loom dna add <nombre>") + "\n")
	}
	for _, d := range m.DNA {
		fmt.Fprintf(&b, "  %-16s %s\n", d.Nombre, e.f(e.Apagado, corto(d.Resumen, 70)))
	}

	c := m.Compuertas
	var partes []string
	for _, est := range []string{"lista", "en_curso", "pendiente", "detenida"} {
		if n := c.Tareas[est]; n > 0 {
			partes = append(partes, fmt.Sprintf("%s %d %s", e.glifo(est), n, est))
		}
	}
	acept := c.Aceptacion
	switch acept {
	case "verde":
		acept = e.f(e.Ok, acept)
	case "roja":
		acept = e.f(e.Mal, acept)
	default:
		acept = e.f(e.Apagado, acept)
	}
	dnaGate := "dna: sin compuerta"
	if c.DNA {
		dnaGate = "dna: compuerta activa"
	}
	fmt.Fprintf(&b, "\n%s\n  tareas  %s\n  aceptación  %s · %s\n\n", e.f(e.Titulo, "COMPUERTAS"), orVacio(strings.Join(partes, "  ")), acept, dnaGate)

	b.WriteString(e.f(e.Titulo, "MÓDULOS") + "\n")
	for i, mod := range m.Modulos {
		rama, hijo := "├─ ", "│  "
		if i == len(m.Modulos)-1 {
			rama, hijo = "└─ ", "   "
		}
		fmt.Fprintf(&b, "%s%s %s\n", rama, mod.Ruta, e.f(e.Apagado, fmt.Sprintf("%d archivos · %d símbolos", mod.Archivos, mod.NumSimbolos)))
		for _, t := range mod.Tareas {
			fmt.Fprintf(&b, "%s%s %s %s\n", hijo, e.glifo(t.Estado), t.ID, corto(t.Titulo, 60))
		}
		for _, c := range mod.Expone {
			linea := fmt.Sprintf("%sexpone  %s", hijo, corto(c.Firma, 70))
			if len(c.Consumidores) > 0 {
				linea += e.f(e.Apagado, "  ← "+strings.Join(c.Consumidores, ", "))
			}
			b.WriteString(linea + "\n")
		}
		if len(mod.Expone) == 0 && len(mod.Simbolos) > 0 {
			api := make([]string, 0, 5)
			for _, s := range mod.Simbolos {
				if len(api) < 5 {
					api = append(api, corto(s, 40))
				}
			}
			extra := ""
			if mod.NumSimbolos > len(api) {
				extra = fmt.Sprintf(" (+%d)", mod.NumSimbolos-len(api))
			}
			fmt.Fprintf(&b, "%s%s  %s\n", hijo, e.f(e.Apagado, "api"), strings.Join(api, " · ")+extra)
		}
		if len(mod.Importa) > 0 {
			fmt.Fprintf(&b, "%simporta → %s\n", hijo, lista(mod.Importa))
		}
		if len(mod.Consume) > 0 {
			fmt.Fprintf(&b, "%sconsume → %s\n", hijo, lista(mod.Consume))
		}
		if len(mod.UsadoPor) > 0 {
			fmt.Fprintf(&b, "%susado por ← %s\n", hijo, lista(mod.UsadoPor))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

var noId = regexp.MustCompile(`[^A-Za-z0-9]+`)

// Mermaid draws the map as a flowchart: solid arrows are imports, dashed
// ones are contracts a task uses; nodes are colored by their tasks' state.
func Mermaid(m Mapa) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	if len(m.DNA) > 0 {
		var n []string
		for _, d := range m.DNA {
			n = append(n, d.Nombre)
		}
		fmt.Fprintf(&b, "  %%%% dna: %s\n", strings.Join(n, ", "))
	}
	ids := map[string]string{}
	for i, mod := range m.Modulos {
		id := fmt.Sprintf("m%d_%s", i, strings.Trim(noId.ReplaceAllString(mod.Ruta, "_"), "_"))
		ids[mod.Ruta] = id
		etiqueta := mod.Ruta
		if t := resumenTareas(mod.Tareas); t != "" {
			etiqueta += "<br/>" + t
		}
		if len(mod.Expone) > 0 {
			var f []string
			for _, c := range mod.Expone {
				f = append(f, escapar(corto(c.Firma, 40)))
			}
			etiqueta += "<br/>" + strings.Join(f, "<br/>")
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, strings.ReplaceAll(etiqueta, `"`, "'"))
	}
	for _, a := range m.Aristas {
		de, para := ids[a.De], ids[a.A]
		if de == "" || para == "" {
			continue
		}
		if a.Tipo == "uses" {
			fmt.Fprintf(&b, "  %s -. \"%s\" .-> %s\n", de, escapar(a.Etiqueta), para)
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", de, para)
		}
	}
	b.WriteString("  classDef lista fill:#d8f0ea,stroke:#4FB3A2;\n  classDef detenida fill:#f6d5cc,stroke:#d96c4a;\n  classDef en_curso fill:#f3e6b8,stroke:#c9a227;\n")
	clases := map[string][]string{}
	for _, mod := range m.Modulos {
		if c := claseDe(mod.Tareas); c != "" {
			clases[c] = append(clases[c], ids[mod.Ruta])
		}
	}
	var ks []string
	for k := range clases {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "  class %s %s;\n", strings.Join(clases[k], ","), k)
	}
	return strings.TrimRight(b.String(), "\n")
}

// claseDe is the worst state among a module's tasks, or "" if none stands out.
func claseDe(ts []Tarea) string {
	if len(ts) == 0 {
		return ""
	}
	hay := map[string]bool{}
	for _, t := range ts {
		hay[t.Estado] = true
	}
	switch {
	case hay["detenida"]:
		return "detenida"
	case hay["en_curso"]:
		return "en_curso"
	case hay["lista"] && !hay["pendiente"]:
		return "lista"
	}
	return ""
}

func resumenTareas(ts []Tarea) string {
	var p []string
	for _, t := range ts {
		p = append(p, glifo(t.Estado)+" "+t.ID)
	}
	return strings.Join(p, " ")
}

func escapar(s string) string {
	return strings.NewReplacer(`"`, "'", "<", "‹", ">", "›").Replace(s)
}

func orVacio(s string) string {
	if s == "" {
		return "ninguna"
	}
	return s
}

func corto(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// lista joins names, cutting long lists so a hub module stays one line.
func lista(s []string) string {
	const max = 6
	if len(s) <= max {
		return strings.Join(s, ", ")
	}
	return strings.Join(s[:max], ", ") + fmt.Sprintf(" (+%d)", len(s)-max)
}
