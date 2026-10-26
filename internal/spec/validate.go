package spec

import (
	"fmt"
	"path"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

type Issue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidatePlan revisa el IR antes de gastar tokens. Los errores son
// invariantes mecánicas; la cobertura semántica se reporta como advertencia
// porque no debe fingirse una prueba determinista basada solo en palabras.
//
// previas son las tareas que ya viven en el repo: un plan nuevo sobre un
// proyecto en marcha depende de ellas y consume lo que expusieron. Cuentan
// como proveedoras y como dependencias, pero no se revalidan ni entran al
// cruce de alcances: ya se entregaron o tienen su propia corrida.
func ValidatePlan(s Spec, tasks []task.Task, previas ...task.Task) []Issue {
	var out []Issue
	byID := map[string]task.Task{}
	exposed := map[string]string{}
	nuevas := map[string]bool{}
	for _, t := range tasks {
		nuevas[t.ID] = true
	}
	previa := map[string]bool{}
	for _, t := range previas {
		if nuevas[t.ID] {
			continue // se reescribe: vale la versión del plan
		}
		previa[t.ID] = true
		for _, x := range t.Expone {
			exposed[x] = t.ID
		}
	}
	for _, t := range tasks {
		byID[t.ID] = t
		for _, x := range t.Expone {
			if prev := exposed[x]; prev != "" && prev != t.ID && !previa[prev] {
				out = append(out, Issue{"error", "duplicate_interface", fmt.Sprintf("%s y %s exponen %q", prev, t.ID, x)})
			}
			exposed[x] = t.ID
		}
	}
	canonicas := map[string]bool{}
	for x := range exposed {
		canonicas[firmaCanonica(x)] = true
	}
	for _, t := range tasks {
		for _, d := range t.DependeDe {
			if _, ok := byID[d]; !ok && !previa[d] {
				out = append(out, Issue{"error", "missing_dependency", fmt.Sprintf("%s depende de %s, que no existe", t.ID, d)})
			}
		}
		for _, u := range t.Usa {
			if exposed[u] == "" && !canonicas[firmaCanonica(u)] {
				name := task.NombreDeFirma(u)
				var similar string
				for signature := range exposed {
					if task.NombreDeFirma(signature) == name {
						similar = signature
						break
					}
				}
				if similar != "" {
					out = append(out, Issue{"error", "incompatible_interface", fmt.Sprintf("%s usa %q pero el plan expone %q", t.ID, u, similar)})
				} else {
					out = append(out, Issue{"error", "orphan_interface", fmt.Sprintf("%s usa %q y ninguna tarea lo expone", t.ID, u)})
				}
			}
		}
	}
	state := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		if state[id] == 1 {
			out = append(out, Issue{"error", "cycle", "dependencia circular que incluye " + id})
			return
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		for _, d := range byID[id].DependeDe {
			if _, ok := byID[d]; ok {
				visit(d)
			}
		}
		state[id] = 2
	}
	for id := range byID {
		visit(id)
	}
	// dos tareas encadenadas por depende_de corren en fila: la segunda
	// arranca sobre lo que dejó la primera, así que compartir archivos no
	// choca. El esqueleto crea los stubs que después rellena cada tarea.
	encadenadas := func(a, b string) bool {
		vistos := map[string]bool{}
		var alcanza func(string) bool
		alcanza = func(id string) bool {
			if id == b {
				return true
			}
			if vistos[id] {
				return false
			}
			vistos[id] = true
			for _, d := range byID[id].DependeDe {
				if alcanza(d) {
					return true
				}
			}
			return false
		}
		return alcanza(a)
	}
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if encadenadas(tasks[i].ID, tasks[j].ID) || encadenadas(tasks[j].ID, tasks[i].ID) {
				continue
			}
			for _, a := range tasks[i].TocarSolo {
				for _, b := range tasks[j].TocarSolo {
					if globsOverlap(a, b) {
						out = append(out, Issue{"error", "write_overlap", fmt.Sprintf("%s y %s pueden escribir la misma zona (%s / %s)", tasks[i].ID, tasks[j].ID, a, b)})
					}
				}
			}
		}
	}
	// cubre es exacto; las palabras en común, solo una pista
	cubiertos := map[string]bool{}
	for _, t := range tasks {
		for _, c := range t.Cubre {
			cubiertos[c] = true
		}
	}
	ids := s.IDs()
	for i, r := range s.Requirements {
		if !cubiertos[ids[i]] && !covered(r, tasks) {
			out = append(out, Issue{"warning", "requirement_coverage", "ninguna tarea declara cobertura reconocible para: " + r})
		}
	}
	for _, a := range s.Acceptance {
		if a.Command == "" && !covered(a.Criterion, tasks) {
			out = append(out, Issue{"warning", "acceptance_coverage", "criterio sin comando ni cobertura reconocible: " + a.Criterion})
		}
	}
	if len(tasks) > 1 && len(s.Acceptance) == 0 {
		out = append(out, Issue{"warning", "integration_test", "el feature tiene varias tareas y no declara aceptación global"})
	}
	return dedupeIssues(out)
}

func globsOverlap(a, b string) bool {
	a = strings.TrimSuffix(a, "/**")
	b = strings.TrimSuffix(b, "/**")
	if a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") {
		return true
	}
	ma, _ := path.Match(a, b)
	mb, _ := path.Match(b, a)
	return ma || mb
}
func covered(q string, ts []task.Task) bool {
	words := strings.Fields(strings.ToLower(q))
	for _, t := range ts {
		hay := strings.ToLower(t.Titulo + " " + t.Porque + " " + t.Notas + " " + t.ListoCuando)
		hits := 0
		for _, w := range words {
			w = strings.Trim(w, ".,:;¿?¡!")
			if len(w) > 4 && strings.Contains(hay, w) {
				hits++
			}
		}
		if hits >= 1 {
			return true
		}
	}
	return false
}
func dedupeIssues(xs []Issue) []Issue {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		k := x.Code + x.Message
		if !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}

// firmaCanonica quita de una firma lo que no cambia el contrato para
// quien la consume: la palabra clave de declaración ("def", "func") y
// la lista de bases de una clase. El modelo expuso "class Garment(Base):
// id, ..." y en usa copió "class Garment: id, ...", y el plan entero se
// rechazaba por incompatible. Los tipos sí se comparan: "Iterator" y
// "Generator" siguen siendo firmas distintas.
func firmaCanonica(firma string) string {
	s := strings.Join(strings.Fields(firma), " ")
	for _, kw := range []string{"async def ", "def ", "func ", "function "} {
		s = strings.TrimPrefix(s, kw)
	}
	if strings.HasPrefix(s, "class ") {
		if i := strings.IndexAny(s, "(:"); i >= 0 && s[i] == '(' {
			if j := strings.Index(s[i:], ")"); j >= 0 {
				s = s[:i] + s[i+j+1:]
			}
		}
	}
	return s
}
