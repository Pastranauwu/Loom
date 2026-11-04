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
	Proposito   string     `json:"proposito,omitempty"` // what it is for, from the package comment or its entry file
	Contenido   []Parte    `json:"contenido,omitempty"` // each file that says what it holds
	Lenguaje    string     `json:"lenguaje,omitempty"`  // dominant file extension, e.g. ".py"
	Archivos    int        `json:"archivos"`
	NumSimbolos int        `json:"simbolos"`
	Simbolos    []string   `json:"api,omitempty"` // first exported symbols, from the graph
	Tareas      []Tarea    `json:"tareas,omitempty"`
	Expone      []Contrato `json:"expone,omitempty"`
	Importa     []string   `json:"importa,omitempty"`
	UsadoPor    []string   `json:"usado_por,omitempty"`
	Consume     []string   `json:"consume,omitempty"` // modules whose contracts its tasks use
	Capa        int        `json:"capa"`              // 0 = entry points: nobody depends on them
}

// Parte is one file of a module with the line its header declares.
type Parte struct {
	Archivo string `json:"archivo"` // relative to the module
	Doc     string `json:"doc"`
}

const maxContenido = 8

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
	NumCapas   int        `json:"capas"`
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
		exts := map[string]map[string]int{}
		docDe := map[string]string{} // module -> file its purpose comes from
		conDoc := map[string][]string{}
		for f, n := range e.Grafo.Files {
			r := mod(path.Dir(f))
			m := get(r)
			m.Archivos++
			m.NumSimbolos += len(n.Symbols)
			m.Simbolos = append(m.Simbolos, n.Symbols...)
			if exts[r] == nil {
				exts[r] = map[string]int{}
			}
			exts[r][path.Ext(f)]++
			if n.Doc == "" {
				continue
			}
			conDoc[r] = append(conDoc[r], f)
			if hablaPorElModulo(f, r) && (docDe[r] == "" || mejorDoc(f, docDe[r])) {
				docDe[r], m.Proposito = f, n.Doc
			}
		}
		for r, c := range exts {
			for x, n := range c {
				if l := mods[r].Lenguaje; n > c[l] || n == c[l] && x < l {
					mods[r].Lenguaje = x
				}
			}
		}
		for r, fs := range conDoc {
			m := mods[r]
			if m.Proposito == "" && m.Archivos == 1 {
				docDe[r], m.Proposito = fs[0], e.Grafo.Files[fs[0]].Doc
			}
			sort.Strings(fs)
			for _, f := range fs {
				if f != docDe[r] && len(m.Contenido) < maxContenido {
					m.Contenido = append(m.Contenido, Parte{Archivo: strings.TrimPrefix(f, r+"/"), Doc: e.Grafo.Files[f].Doc})
				}
			}
		}
		// outside Go an import names a file stem or, like Python's
		// `from . import x`, a package directory itself
		dirs := map[string]bool{}
		for f := range e.Grafo.Files {
			dirs[path.Dir(f)] = true
		}
		for f, n := range e.Grafo.Files {
			de := mod(path.Dir(f))
			for _, imp := range n.Imports {
				dir := imp
				if path.Ext(f) != ".go" && !dirs[imp] {
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
		ordenarApi(m.Simbolos)
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

	n := asignarCapas(out, finales)
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
	return Mapa{Modulos: out, Aristas: finales, DNA: dd, Compuertas: c, NumCapas: n}
}

// hablaPorElModulo says whether a file's header describes its whole module
// and not only itself: a Go package comment, doc.go, __init__.py or an
// index file at the module root.
func hablaPorElModulo(f, modulo string) bool {
	if path.Dir(f) != modulo && !(modulo == "." && path.Dir(f) == ".") {
		return false
	}
	switch b := path.Base(f); {
	case path.Ext(b) == ".go", b == "__init__.py":
		return true
	case strings.HasPrefix(b, "index."):
		return true
	}
	return false
}

// mejorDoc says whether f describes its module better than actual: the
// shallowest file wins, then doc.go, then the path order. Deterministic.
func mejorDoc(f, actual string) bool {
	pf, pa := strings.Count(f, "/"), strings.Count(actual, "/")
	if pf != pa {
		return pf < pa
	}
	if df, da := path.Base(f) == "doc.go", path.Base(actual) == "doc.go"; df != da {
		return df
	}
	return f < actual
}

// ordenarApi puts types first, then free functions, then methods and the
// rest, so the few symbols that fit say what the module offers.
func ordenarApi(s []string) {
	peso := func(x string) int {
		for _, p := range []string{"export ", "default ", "async "} {
			x = strings.TrimPrefix(x, p)
		}
		switch {
		case strings.HasPrefix(x, "type "), strings.HasPrefix(x, "class "), strings.HasPrefix(x, "interface "), strings.HasPrefix(x, "enum "):
			return 0
		case strings.HasPrefix(x, "func ("):
			return 2
		case strings.HasPrefix(x, "func "), strings.HasPrefix(x, "function "), strings.HasPrefix(x, "def "):
			return 1
		}
		return 3
	}
	sort.SliceStable(s, func(i, j int) bool {
		if a, b := peso(s[i]), peso(s[j]); a != b {
			return a < b
		}
		return s[i] < s[j]
	})
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

// EsAndamio reconoce pruebas, migraciones y fixtures: sostienen el código
// pero no son bloques de la arquitectura. No cuentan para las capas: si una
// prueba importa un módulo, ese módulo no baja por eso.
func EsAndamio(ruta string) bool {
	switch path.Base(ruta) {
	case "tests", "test", "__tests__", "spec", "specs", "e2e", "migrations", "fixtures", "testdata":
		return true
	}
	return false
}

// asignarCapas puts each module one layer below the deepest module that
// depends on it, so entry points land on layer 0 and shared foundations at
// the bottom. A cycle is cut where it is found. It returns the layer count.
func asignarCapas(mods []Modulo, aristas []Arista) int {
	dependientes := map[string][]string{}
	for _, a := range aristas {
		if !EsAndamio(a.De) {
			dependientes[a.A] = append(dependientes[a.A], a.De)
		}
	}
	capa := map[string]int{}
	enCurso := map[string]bool{}
	var prof func(string) int
	prof = func(r string) int {
		if c, ok := capa[r]; ok {
			return c
		}
		if enCurso[r] {
			return 0
		}
		enCurso[r] = true
		c := 0
		for _, d := range dependientes[r] {
			if v := prof(d) + 1; v > c {
				c = v
			}
		}
		enCurso[r] = false
		capa[r] = c
		return c
	}
	max := 0
	for i := range mods {
		mods[i].Capa = prof(mods[i].Ruta)
		if mods[i].Capa > max {
			max = mods[i].Capa
		}
	}
	if len(mods) == 0 {
		return 0
	}
	return max + 1
}
