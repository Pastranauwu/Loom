package topo

import (
	"fmt"
	"path"
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

	b.WriteString(e.f(e.Titulo, "MÓDULOS") + "  " + e.f(e.Apagado, Leyenda) + "\n")
	for capa := 0; capa < m.NumCapas; capa++ {
		var mods []Modulo
		for _, mod := range m.Modulos {
			if mod.Capa == capa {
				mods = append(mods, mod)
			}
		}
		if len(mods) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", e.f(e.Titulo, NombreCapa(capa, m.NumCapas)))
		for i, mod := range mods {
			rama, hijo := "├─ ", "│  "
			if i == len(mods)-1 {
				rama, hijo = "└─ ", "   "
			}
			fmt.Fprintf(&b, "%s%s %s\n", rama, mod.Ruta, e.f(e.Apagado, fmt.Sprintf("%d archivos · %d símbolos", mod.Archivos, mod.NumSimbolos)))
			if mod.Proposito != "" {
				fmt.Fprintf(&b, "%s%s\n", hijo, corto(mod.Proposito, 100))
			}
			for i, p := range mod.Contenido {
				if i == 4 {
					fmt.Fprintf(&b, "%s  (+%d archivos)\n", hijo, len(mod.Contenido)-4)
					break
				}
				fmt.Fprintf(&b, "%s  %s · %s\n", hijo, p.Archivo, corto(p.Doc, 70))
			}
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
			// sin propósito ni contratos, unos símbolos dan la pista de qué ofrece
			if mod.Proposito == "" && len(mod.Contenido) == 0 && len(mod.Expone) == 0 && len(mod.Simbolos) > 0 {
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
	}
	b.WriteString("\n" + e.f(e.Apagado, "detalle de un módulo: loom map <módulo>"))
	return strings.TrimRight(b.String(), "\n")
}

// Leyenda explains the layers in one line: it is the abstraction the map
// draws, and without it the tree reads as a flat list.
const Leyenda = "capas de arriba abajo · arriba lo que nadie importa (entradas), abajo en lo que todos se apoyan (cimientos); cada módulo depende de los de más abajo"

// NombreCapa names a layer for a human: entry points, foundations or a
// numbered middle layer.
func NombreCapa(capa, total int) string {
	switch {
	case total > 1 && capa == 0:
		return "CAPA 1 · entradas"
	case total > 1 && capa == total-1:
		return fmt.Sprintf("CAPA %d · cimientos", capa+1)
	}
	return fmt.Sprintf("CAPA %d", capa+1)
}

// Detalle explains one module, for a person or a model asking what it is
// for: its purpose, its place in the layers, who it depends on and who
// leans on it, its contracts and tasks, and each file with its comment and
// full public API. consulta is a module path, a file inside it or the last
// segment of the path; an ambiguous one lists the candidates.
func Detalle(e Entrada, consulta string) (string, error) {
	e.Todos = true
	m := Construir(e)
	mod, err := buscar(m, consulta)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s de %d\n", mod.Ruta, strings.ToLower(NombreCapa(mod.Capa, m.NumCapas)), m.NumCapas)
	if mod.Proposito != "" {
		fmt.Fprintf(&b, "propósito: %s\n", mod.Proposito)
	} else {
		b.WriteString("propósito: sin comentario de paquete · se deduce de su API\n")
	}
	fmt.Fprintf(&b, "%d archivos · %d símbolos\n", mod.Archivos, mod.NumSimbolos)
	fmt.Fprintf(&b, "depende de: %s\n", orNada(conProposito(m, mod.Importa)))
	var usan, prueban []string
	for _, r := range mod.UsadoPor {
		if EsAndamio(r) {
			prueban = append(prueban, r)
		} else {
			usan = append(usan, r)
		}
	}
	fmt.Fprintf(&b, "lo usan: %s\n", orNada(conProposito(m, usan)))
	if len(prueban) > 0 {
		fmt.Fprintf(&b, "lo prueban: %s\n", strings.Join(prueban, ", "))
	}
	if len(mod.Consume) > 0 {
		fmt.Fprintf(&b, "consume contratos de: %s\n", strings.Join(mod.Consume, ", "))
	}
	for _, c := range mod.Expone {
		fmt.Fprintf(&b, "expone %s (%s)", c.Firma, c.Tarea)
		if len(c.Consumidores) > 0 {
			fmt.Fprintf(&b, " ← %s", strings.Join(c.Consumidores, ", "))
		}
		b.WriteString("\n")
	}
	for _, t := range mod.Tareas {
		fmt.Fprintf(&b, "tarea %s %s %s\n", glifo(t.Estado), t.ID, t.Titulo)
	}
	if e.Grafo != nil {
		var fs []string
		for f := range e.Grafo.Files {
			if truncar(path.Dir(f), profundidad(e)) == mod.Ruta {
				fs = append(fs, f)
			}
		}
		sort.Strings(fs)
		if len(fs) > 0 {
			b.WriteString("archivos:\n")
		}
		for _, f := range fs {
			n := e.Grafo.Files[f]
			linea := "  " + strings.TrimPrefix(f, mod.Ruta+"/")
			if n.Doc != "" {
				linea += " · " + n.Doc
			}
			b.WriteString(linea + "\n")
			api := append([]string(nil), n.Symbols...)
			ordenarApi(api)
			for _, s := range api {
				b.WriteString("    " + s + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func profundidad(e Entrada) int {
	if e.Profundidad < 1 {
		return 2
	}
	return e.Profundidad
}

func buscar(m Mapa, q string) (Modulo, error) {
	q = strings.Trim(path.Clean(q), "/")
	var cand []Modulo
	for _, mod := range m.Modulos {
		if mod.Ruta == q {
			return mod, nil
		}
		if strings.HasPrefix(q, mod.Ruta+"/") || path.Base(mod.Ruta) == q {
			cand = append(cand, mod)
		}
	}
	switch len(cand) {
	case 1:
		return cand[0], nil
	case 0:
		return Modulo{}, fmt.Errorf("no hay módulo %q · loom map --all lista los que hay", q)
	}
	var rutas []string
	for _, c := range cand {
		rutas = append(rutas, c.Ruta)
	}
	return Modulo{}, fmt.Errorf("%q es ambiguo · elige uno: %s", q, strings.Join(rutas, ", "))
}

// conProposito names each neighbour with its purpose, so the model sees
// what the module leans on without asking module by module.
func conProposito(m Mapa, rutas []string) string {
	por := map[string]string{}
	for _, mod := range m.Modulos {
		por[mod.Ruta] = mod.Proposito
	}
	var out []string
	for _, r := range rutas {
		if p := por[r]; p != "" {
			r += " (" + corto(p, 60) + ")"
		}
		out = append(out, r)
	}
	return strings.Join(out, "; ")
}

func orNada(s string) string {
	if s == "" {
		return "nada"
	}
	return s
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
		if mod.Proposito != "" {
			etiqueta += "<br/><i>" + escapar(corto(mod.Proposito, 50)) + "</i>"
		}
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
