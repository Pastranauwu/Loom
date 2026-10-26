// Package topo builds the topological view of a project: its modules,
// the public contracts each one offers (exposes), who consumes whom (uses
// and imports), the active DNA and the state of the gates. It reads the
// persistent repo graph and the task contracts; it opens no source file.
package topo

import (
	"path"
	"sort"
	"strings"

	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/graph"
	"github.com/Pastranauwu/loom/internal/task"
)

type Tarea struct {
	ID     string `json:"id"`
	Titulo string `json:"titulo"`
	Estado string `json:"estado"`
}

type Contrato struct {
	Firma        string   `json:"firma"`
	Tarea        string   `json:"tarea"`
	Consumidores []string `json:"consumidores,omitempty"`
}

type Modulo struct {
	Ruta        string     `json:"ruta"`
	Archivos    int        `json:"archivos"`
	NumSimbolos int        `json:"simbolos"`
	Simbolos    []string   `json:"api,omitempty"` // first exported symbols, from the graph
	Tareas      []Tarea    `json:"tareas,omitempty"`
	Expone      []Contrato `json:"expone,omitempty"`
	Importa     []string   `json:"importa,omitempty"`
	UsadoPor    []string   `json:"usado_por,omitempty"`
	Consume     []string   `json:"consume,omitempty"` // modules whose contracts its tasks use
}

type Arista struct {
	De       string `json:"de"`
	A        string `json:"a"`
	Tipo     string `json:"tipo"` // import | uses
	Etiqueta string `json:"etiqueta,omitempty"`
}

type DNA struct {
	Nombre  string `json:"nombre"`
	Resumen string `json:"resumen"`
}

type Compuertas struct {
	Tareas     map[string]int `json:"tareas"`
	Aceptacion string         `json:"aceptacion"` // verde | roja | sin correr
	Motivo     string         `json:"motivo,omitempty"`
	DNA        bool           `json:"dna"` // the dna exit-gate step is active
}

type Mapa struct {
	Modulos    []Modulo   `json:"modulos"`
	Aristas    []Arista   `json:"aristas"`
	DNA        []DNA      `json:"dna"`
	Compuertas Compuertas `json:"compuertas"`
}

// Entrada is everything the map is built from; Grafo may be nil.
type Entrada struct {
	Grafo       *graph.Graph
	Tareas      []task.Task
	Estados     map[string]string // task id -> estado
	DNA         []dna.Arquetipo   // the active archetypes
	Aceptacion  string
	Motivo      string
	Profundidad int  // path segments that name a module (default 2)
	Todos       bool // include modules with no task or contract
}

const maxApi = 12

// Construir joins the graph and the contracts into a Mapa.
func Construir(e Entrada) Mapa {
	prof := e.Profundidad
	if prof < 1 {
		prof = 2
	}
	mod := func(dir string) string { return truncar(dir, prof) }
	mods := map[string]*Modulo{}
	get := func(r string) *Modulo {
		if mods[r] == nil {
			mods[r] = &Modulo{Ruta: r}
		}
		return mods[r]
	}

	var aristas []Arista
	visto := map[Arista]bool{}
	arista := func(a Arista) {
		if a.De != a.A && !visto[a] {
			visto[a] = true
			aristas = append(aristas, a)
		}
	}

	if e.Grafo != nil {
		for f, n := range e.Grafo.Files {
			m := get(mod(path.Dir(f)))
			m.Archivos++
			m.NumSimbolos += len(n.Symbols)
			m.Simbolos = append(m.Simbolos, n.Symbols...)
		}
		for f, n := range e.Grafo.Files {
			de := mod(path.Dir(f))
			for _, imp := range n.Imports {
				dir := imp
				if path.Ext(f) != ".go" {
					dir = path.Dir(imp)
				}
				if a := mod(dir); mods[a] != nil {
					arista(Arista{De: de, A: a, Tipo: "import"})
				}
			}
		}
	}

	// contracts: each task lives in the module its scope points to
	moduloDe := map[string]string{}
	for _, t := range e.Tareas {
		r := mod(dirDeAlcance(t.TocarSolo))
		moduloDe[t.ID] = r
		m := get(r)
		m.Tareas = append(m.Tareas, Tarea{ID: t.ID, Titulo: t.Titulo, Estado: e.Estados[t.ID]})
		for _, sig := range t.Expone {
			m.Expone = append(m.Expone, Contrato{Firma: strings.TrimSpace(sig), Tarea: t.ID})
		}
	}
	for _, u := range e.Tareas {
		for _, sig := range u.Usa {
			for _, p := range e.Tareas {
				for i, c := range mods[moduloDe[p.ID]].Expone {
					if c.Tarea != p.ID || !mismaFirma(c.Firma, sig) {
						continue
					}
					mods[moduloDe[p.ID]].Expone[i].Consumidores = append(c.Consumidores, u.ID)
					arista(Arista{De: moduloDe[u.ID], A: moduloDe[p.ID], Tipo: "uses", Etiqueta: task.NombreDeFirma(sig)})
				}
			}
		}
	}

	hayTareas := len(e.Tareas) > 0
	var out []Modulo
	incluidos := map[string]bool{}
	for r, m := range mods {
		if !e.Todos && hayTareas && len(m.Tareas) == 0 && len(m.Expone) == 0 {
			continue
		}
		sort.Strings(m.Simbolos)
		if len(m.Simbolos) > maxApi {
			m.Simbolos = m.Simbolos[:maxApi]
		}
		incluidos[r] = true
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ruta < out[j].Ruta })

	var finales []Arista
	for _, a := range aristas {
		if incluidos[a.De] && incluidos[a.A] {
			finales = append(finales, a)
		}
	}
	sort.Slice(finales, func(i, j int) bool {
		x, y := finales[i], finales[j]
		if x.De != y.De {
			return x.De < y.De
		}
		if x.A != y.A {
			return x.A < y.A
		}
		return x.Tipo+x.Etiqueta < y.Tipo+y.Etiqueta
	})
	idx := map[string]*Modulo{}
	for i := range out {
		idx[out[i].Ruta] = &out[i]
	}
	for _, a := range finales {
		if a.Tipo == "import" {
			idx[a.De].Importa = unico(append(idx[a.De].Importa, a.A))
			idx[a.A].UsadoPor = unico(append(idx[a.A].UsadoPor, a.De))
		} else {
			idx[a.De].Consume = unico(append(idx[a.De].Consume, a.A))
		}
	}

	c := Compuertas{Tareas: map[string]int{}, Aceptacion: e.Aceptacion, Motivo: e.Motivo}
	if c.Aceptacion == "" {
		c.Aceptacion = "sin correr"
	}
	for _, t := range e.Tareas {
		c.Tareas[e.Estados[t.ID]]++
	}
	var dd []DNA
	for _, a := range e.DNA {
		dd = append(dd, DNA{Nombre: a.Nombre, Resumen: a.Resumen()})
	}
	c.DNA = len(dd) > 0
	return Mapa{Modulos: out, Aristas: finales, DNA: dd, Compuertas: c}
}

// truncar keeps the first n segments of a directory.
func truncar(dir string, n int) string {
	dir = strings.Trim(path.Clean(dir), "/")
	if dir == "" || dir == "." {
		return "."
	}
	p := strings.Split(dir, "/")
	if len(p) > n {
		p = p[:n]
	}
	return strings.Join(p, "/")
}

// dirDeAlcance is the directory a task's non-test scope points to.
func dirDeAlcance(tocar []string) string {
	for _, g := range tocar {
		if task.EsArchivoDePrueba(g) {
			continue
		}
		if i := strings.IndexAny(g, "*?["); i >= 0 {
			g = g[:i]
		}
		if !strings.HasSuffix(g, "/") {
			g = path.Dir(g)
		}
		return strings.TrimSuffix(g, "/")
	}
	return "."
}

func mismaFirma(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == b {
		return true
	}
	na, nb := task.NombreDeFirma(a), task.NombreDeFirma(b)
	return na != "" && na == nb
}

func unico(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}
