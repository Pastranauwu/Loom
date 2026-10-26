package spec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const EstadoFile = "state.json"

const (
	ClaseRequirement = "requirement"
	ClaseRegla       = "rule"
	ClaseAceptacion  = "acceptance"
	ClaseRestriccion = "constraint"
	ClaseTarea       = "task"
)

var clases = []string{ClaseRequirement, ClaseRegla, ClaseAceptacion, ClaseRestriccion, ClaseTarea}

type Pieza struct {
	Hash  string `json:"hash"`
	Texto string `json:"texto"`
}

type EstadoSpec struct {
	Feature string                      `json:"feature,omitempty"`
	Piezas  map[string]map[string]Pieza `json:"piezas"`
}

type Estado struct {
	Specs map[string]EstadoSpec `json:"specs"`
}

type Cambio struct {
	Clase, Clave, Antes, Ahora string
}

type Delta struct {
	Agregado, Modificado, Eliminado []Cambio
}

func (d Delta) Vacio() bool {
	return len(d.Agregado)+len(d.Modificado)+len(d.Eliminado) == 0
}

func (d Delta) De(clase string) Delta {
	var o Delta
	for _, c := range d.Agregado {
		if c.Clase == clase {
			o.Agregado = append(o.Agregado, c)
		}
	}
	for _, c := range d.Modificado {
		if c.Clase == clase {
			o.Modificado = append(o.Modificado, c)
		}
	}
	for _, c := range d.Eliminado {
		if c.Clase == clase {
			o.Eliminado = append(o.Eliminado, c)
		}
	}
	return o
}

func (d Delta) String() string {
	var b strings.Builder
	for _, g := range []struct {
		marca string
		cs    []Cambio
	}{{"+", d.Agregado}, {"~", d.Modificado}, {"-", d.Eliminado}} {
		for _, c := range g.cs {
			texto := c.Ahora
			if g.marca == "-" {
				texto = c.Antes
			}
			fmt.Fprintf(&b, "%s %s %s · %s\n", g.marca, c.Clase, c.Clave, resumir(texto))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func resumir(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 100 {
		return s[:100] + "…"
	}
	return s
}

func hashDe(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))[:12]
}

func pieza(texto string) Pieza { return Pieza{Hash: hashDe(texto), Texto: texto} }

func textoAceptacion(a Acceptance) string {
	if a.Command == "" {
		return a.Criterion
	}
	return a.Criterion + " · comando: " + a.Command
}

// Snapshot resume lo que el humano declaró en el spec: una pieza con hash
// por requirement, regla, aceptación, restricción y tarea.
func Snapshot(s Spec) EstadoSpec {
	p := map[string]map[string]Pieza{}
	for _, c := range clases {
		p[c] = map[string]Pieza{}
	}
	for i, id := range s.IDs() {
		p[ClaseRequirement][id] = pieza(s.Requirements[i])
	}
	for _, r := range s.Reglas {
		p[ClaseRegla][hashDe(r)] = pieza(r)
	}
	for _, a := range s.Acceptance {
		p[ClaseAceptacion][a.Criterion] = pieza(textoAceptacion(a))
	}
	for _, g := range s.Constraints.NoTocar {
		p[ClaseRestriccion][g] = pieza(g)
	}
	for _, t := range s.Tasks {
		clave := t.ID
		if clave == "" {
			clave = t.Titulo
		}
		b, _ := json.Marshal(t)
		p[ClaseTarea][clave] = Pieza{Hash: hashDe(string(b)), Texto: t.Titulo}
	}
	return EstadoSpec{Feature: s.Feature, Piezas: p}
}

// Diff compara lo declarado ahora contra lo último aplicado.
func Diff(prev, cur EstadoSpec) Delta {
	var d Delta
	for _, clase := range clases {
		antes, ahora := prev.Piezas[clase], cur.Piezas[clase]
		for _, k := range claves(ahora) {
			a, ok := antes[k]
			switch {
			case !ok:
				d.Agregado = append(d.Agregado, Cambio{Clase: clase, Clave: k, Ahora: ahora[k].Texto})
			case a.Hash != ahora[k].Hash:
				d.Modificado = append(d.Modificado, Cambio{Clase: clase, Clave: k, Antes: a.Texto, Ahora: ahora[k].Texto})
			}
		}
		for _, k := range claves(antes) {
			if _, ok := ahora[k]; !ok {
				d.Eliminado = append(d.Eliminado, Cambio{Clase: clase, Clave: k, Antes: antes[k].Texto})
			}
		}
	}
	return d
}

func claves(m map[string]Pieza) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func estadoPath(root string) string { return filepath.Join(root, ".loom", EstadoFile) }

func LoadEstado(root string) Estado {
	var e Estado
	if b, err := os.ReadFile(estadoPath(root)); err == nil {
		_ = json.Unmarshal(b, &e)
	}
	if e.Specs == nil {
		e.Specs = map[string]EstadoSpec{}
	}
	return e
}

func saveEstado(root string, e Estado) error {
	if err := os.MkdirAll(filepath.Dir(estadoPath(root)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(estadoPath(root), append(b, '\n'), 0o644)
}

func SaveEstadoSpec(root, clave string, es EstadoSpec) error {
	e := LoadEstado(root)
	e.Specs[clave] = es
	return saveEstado(root, e)
}

// QuitarEstado borra lo aplicado de un spec; sin más specs, borra el archivo.
func QuitarEstado(root, clave string) error {
	e := LoadEstado(root)
	delete(e.Specs, clave)
	if len(e.Specs) == 0 {
		if err := os.Remove(estadoPath(root)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return saveEstado(root, e)
}

// Previo devuelve lo último aplicado de un spec. Sin state.json cae a
// intencion.json y luego a feature.json (repos anteriores a state.json); en
// el último la aceptación no se compara porque el esqueleto agregó la suya.
func Previo(root, clave string, actual Spec) (EstadoSpec, bool) {
	if es, ok := LoadEstado(root).Specs[clave]; ok {
		return es, true
	}
	if i, ok := LoadIntencion(root); ok {
		return Snapshot(Spec{Feature: i.Feature, Requirements: i.Requirements, RequirementIDs: i.IDs, Reglas: i.Reglas, Acceptance: i.Acceptance, Constraints: i.Constraints}), true
	}
	if f, err := LoadFeatureState(root); err == nil && len(f.Requirements) > 0 {
		return Snapshot(Spec{Feature: f.Feature, Requirements: f.Requirements, RequirementIDs: f.RequirementIDs, Reglas: f.Reglas, Acceptance: actual.Acceptance, Constraints: f.Constraints}), true
	}
	return EstadoSpec{}, false
}

// FindAll devuelve los specs vivos: el de la raíz y luego specs/*.yml.
func FindAll(root string) []string {
	var out []string
	if p, err := Find(root); err == nil {
		out = append(out, p)
	}
	for _, pat := range []string{"specs/*.yml", "specs/*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(root, pat))
		sort.Strings(m)
		out = append(out, m...)
	}
	return out
}

// Requirements devuelve textos e ids de los requirements guardados, por id.
func (e EstadoSpec) Requirements() (textos, ids []string) {
	for _, k := range claves(e.Piezas[ClaseRequirement]) {
		ids = append(ids, k)
		textos = append(textos, e.Piezas[ClaseRequirement][k].Texto)
	}
	return textos, ids
}

// FusionarAceptacion deja en feature.json la aceptación de todos los specs:
// la que ya había menos la que este spec aplicó antes, más la de ahora.
func FusionarAceptacion(root string, viejo Spec, prev EstadoSpec, s Spec) error {
	f, err := LoadFeatureState(root)
	if err != nil {
		return err
	}
	propias := prev.Piezas[ClaseAceptacion]
	var out []Acceptance
	for _, a := range viejo.Acceptance {
		if _, mia := propias[a.Criterion]; !mia && !tieneCriterio(f.Acceptance, a.Criterion) {
			out = append(out, a)
		}
	}
	out = append(out, f.Acceptance...)
	f.Acceptance = out
	return SaveFeatureState(root, f)
}

func tieneCriterio(as []Acceptance, criterio string) bool {
	for _, a := range as {
		if a.Criterion == criterio {
			return true
		}
	}
	return false
}
