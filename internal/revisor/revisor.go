// Package revisor implementa la revisión de la entrega antes de
// integrarla: un modelo lee el diff completo y puede vetar el merge.
//
// Es el único paso que juzga intención en vez de mecánica. Las esclusas
// verifican que compila, que pasa, que no hay secretos y que cabe en el
// presupuesto; nada de eso dice si el código hace lo que la tarea pedía.
//
// Falla cerrado, al revés que el examinador: si el modelo no responde, o
// responde algo que no se puede leer, la entrega NO se integra. Perder un
// examen ciego cuesta una prueba; integrar sin revisar mete código sin
// mirar en la rama principal.
package revisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/task"
)

// MaxDiff acota lo que se le manda al modelo. Un diff más grande no se
// revisa a medias: se reporta como no revisable y la entrega se frena.
const MaxDiff = 200_000

// DefaultTimeout es lo que se le da al revisor para responder.
const DefaultTimeout = 5 * time.Minute

// TiposCommit son las categorías que el revisor puede asignar, las de
// Conventional Commits. Acotarlas importa: un tipo inventado no dice
// nada y rompe cualquier changelog que las lea.
var TiposCommit = []string{"feat", "fix", "chore", "docs", "refactor", "test", "perf", "build"}

// Tarea es el juicio del revisor sobre lo que entregó una tarea.
type Tarea struct {
	ID          string   `json:"id"`
	Tipo        string   `json:"tipo"`
	Funciona    bool     `json:"funciona"`
	Descripcion string   `json:"descripcion"`
	Cambios     []string `json:"cambios,omitempty"` // qué corregir, si no funciona
}

// Veredicto es la respuesta del revisor: una línea por tarea, y el
// resumen que sale de ellas.
type Veredicto struct {
	Tareas []Tarea `json:"tareas"`
	Notas  string  `json:"notas,omitempty"` // lo que solo se ve mirando el conjunto
}

// Aprobado reporta si todas las tareas funcionan. No es una aprobación de
// GitHub: la da el humano. Esto solo dice que el revisor no encontró nada
// que corregir.
func (v Veredicto) Aprobado() bool {
	if len(v.Tareas) == 0 {
		return false
	}
	for _, t := range v.Tareas {
		if !t.Funciona {
			return false
		}
	}
	return true
}

// ConCambios devuelve las tareas a las que el revisor les pide algo.
func (v Veredicto) ConCambios() []Tarea {
	var out []Tarea
	for _, t := range v.Tareas {
		if !t.Funciona {
			out = append(out, t)
		}
	}
	return out
}

// Resumen es la línea para la terminal.
func (v Veredicto) Resumen() string {
	pendientes := v.ConCambios()
	if len(pendientes) == 0 {
		return fmt.Sprintf("%d tareas revisadas, ninguna con cambios pedidos", len(v.Tareas))
	}
	var ids []string
	for _, t := range pendientes {
		ids = append(ids, t.ID)
	}
	return fmt.Sprintf("%d de %d tareas necesitan cambios: %s",
		len(pendientes), len(v.Tareas), strings.Join(ids, ", "))
}

// Informe arma el comentario que se deja en el PR: una tabla con el tipo
// y el veredicto de cada tarea, y debajo lo que hay que corregir. Es lo
// que el humano lee antes de darle a aprobar.
func (v Veredicto) Informe() string {
	var b strings.Builder
	b.WriteString("## Revisión de la entrega\n\n")
	b.WriteString("_" + v.Resumen() + "_\n\n")

	b.WriteString("| tarea | tipo | ¿funciona? | qué entrega |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, t := range v.Tareas {
		marca := "sí"
		if !t.Funciona {
			marca = "**no**"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", t.ID, t.Tipo, marca, t.Descripcion)
	}

	for _, t := range v.ConCambios() {
		fmt.Fprintf(&b, "\n### %s · qué cambiar\n\n", t.ID)
		for _, c := range t.Cambios {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}

	if strings.TrimSpace(v.Notas) != "" {
		fmt.Fprintf(&b, "\n### Sobre el conjunto\n\n%s\n", v.Notas)
	}

	b.WriteString("\n---\n")
	if v.Aprobado() {
		b.WriteString("_El revisor no encontró nada que corregir. La aprobación es tuya._\n")
	} else {
		b.WriteString("_El revisor pide cambios. Corrígelos y vuelve a entregar, o apruébalo igual si no estás de acuerdo._\n")
	}
	b.WriteString("\n<sub>revisado por loom · las esclusas ya verificaron que compila, que pasa la suite, que no hay secretos y que cabe en presupuesto</sub>\n")
	return b.String()
}

// Generador pide texto a un modelo. La interfaz mínima, como en plan.
type Generador interface {
	Generar(ctx context.Context, prompt string) (string, error)
}

// Opciones lleva lo que hace falta para revisar una entrega.
type Opciones struct {
	Generador Generador
	Tareas    []task.Task
	Diff      string
	Timeout   time.Duration
}

// Revisar pide el veredicto y lo parsea. Un error significa "no se pudo
// revisar", y quien llama no debe integrar.
func Revisar(ctx context.Context, o Opciones) (Veredicto, error) {
	if o.Generador == nil {
		return Veredicto{}, errors.New(idioma.T("sin revisor · declara el modelo del rol revisor en config.yml"))
	}
	if strings.TrimSpace(o.Diff) == "" {
		return Veredicto{}, errors.New(idioma.T("no hay diff que revisar"))
	}
	if len(o.Diff) > MaxDiff {
		return Veredicto{}, fmt.Errorf(
			"el diff son %d caracteres y el revisor solo cubre %d · revísalo a mano o parte la entrega",
			len(o.Diff), MaxDiff)
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	texto, err := o.Generador.Generar(ctx, Prompt(o.Tareas, o.Diff))
	if err != nil {
		return Veredicto{}, fmt.Errorf(idioma.T("el revisor no respondió · %s"), err)
	}
	return Parse(texto, o.Tareas)
}

// Prompt builds the reviewer's instruction: what was asked and what was written.
func Prompt(tareas []task.Task, diff string) string {
	var b strings.Builder
	b.WriteString("You are loom's reviewer. You decide whether this delivery enters the main branch.\n\n")
	b.WriteString("Code already verified these, do not repeat them: the branch rebases cleanly, each task is one commit, ")
	b.WriteString("the project suite passes on the whole set, there are no secrets or debug prints, and each task ")
	b.WriteString("fits its line budget.\n\n")
	b.WriteString("What YOU judge is what no automatic check sees: whether the code does what the task asked.\n\n")

	b.WriteString("What was asked:\n")
	for _, t := range tareas {
		fmt.Fprintf(&b, "- %s: %s\n", t.ID, t.Titulo)
		if t.Porque != "" {
			fmt.Fprintf(&b, "  why: %s\n", t.Porque)
		}
		fmt.Fprintf(&b, "  considered done when this passes: %s\n", t.ListoCuando)
		if len(t.Expone) > 0 {
			fmt.Fprintf(&b, "  was to expose: %s\n", strings.Join(t.Expone, "; "))
		}
	}

	b.WriteString("\nThe full diff:\n```diff\n")
	b.WriteString(diff)
	b.WriteString("\n```\n\n")

	b.WriteString("For EACH task say whether its implementation works and classify it.\n\n")
	b.WriteString("- \"type\": one of " + strings.Join(TiposCommit, ", ") + ", by what it truly DELIVERS, not by how it is titled. Packaging, Dockerfile or CI are chore; documentation only is docs; fixing something broken is fix; new functionality is feat.\n")
	b.WriteString("- \"description\": one short, concrete sentence of what it delivers, so a human knows what they are approving without reading the diff.\n")
	b.WriteString("- \"works\": false ONLY for defects you can point to in the diff:\n")
	b.WriteString("  · the code does not do what its task said, or only works for the exact case the test checks\n")
	b.WriteString("  · a real failure: broken edge case, unhandled error that loses data, race condition\n")
	b.WriteString("  · a security problem: unvalidated input at a trust boundary, leaked secret, excess permission\n")
	b.WriteString("  · what it delivers does not fit what another task consumes\n")
	b.WriteString("  · the diff deletes, relaxes or rewrites cases of a test that already existed and the task does not declare them in \"Obsolete:\": an old test only changes in what the contract says stopped being valid\n")
	b.WriteString("- \"changes\": if works is false, what to fix, one line per item with file:line. Mandatory: saying something is wrong without saying what to change helps nobody.\n\n")
	b.WriteString("Do NOT mark works:false for style, names, formatting, architecture taste, or for missing tests or documentation nobody asked for. If you doubt and cannot point to the concrete line, it works.\n\n")
	b.WriteString("In \"notes\" put only what is seen by looking at the whole set and fits no single task; empty if there is nothing.\n\n")
	b.WriteString("Return ONLY this JSON, no text around it, with one entry per task above:\n")
	b.WriteString(`{"tasks": [{"id": "T-001", "type": "feat", "works": true, "description": "what it delivers", "changes": []}], "notes": ""}`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// Parse extrae el informe, tolerando vallas markdown y texto alrededor.
// Lo que no se puede leer no se da por revisado: quien decide es el
// humano, y un informe a medias le haría aprobar a ciegas.
func Parse(texto string, esperadas []task.Task) (Veredicto, error) {
	t := strings.TrimSpace(texto)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")

	ini := strings.Index(t, "{")
	fin := strings.LastIndex(t, "}")
	if ini == -1 || fin <= ini {
		return Veredicto{}, errors.New(idioma.T("el revisor no devolvió un informe legible"))
	}
	var v Veredicto
	if err := json.Unmarshal(normalizarInforme([]byte(t[ini:fin+1])), &v); err != nil {
		return Veredicto{}, fmt.Errorf(idioma.T("el informe del revisor no es JSON válido · %s"), err)
	}
	if len(v.Tareas) == 0 {
		return Veredicto{}, errors.New(idioma.T("el revisor no juzgó ninguna tarea"))
	}

	vistas := map[string]bool{}
	for i := range v.Tareas {
		e := &v.Tareas[i]
		e.ID = strings.TrimSpace(e.ID)
		if e.ID == "" {
			return Veredicto{}, errors.New(idioma.T("el revisor devolvió una entrada sin id de tarea"))
		}
		vistas[e.ID] = true
		if !tipoValido(e.Tipo) {
			return Veredicto{}, fmt.Errorf(idioma.T("%s: tipo %q no es de Conventional Commits (%s)"),
				e.ID, e.Tipo, strings.Join(TiposCommit, ", "))
		}
		if strings.TrimSpace(e.Descripcion) == "" {
			return Veredicto{}, fmt.Errorf(idioma.T("%s: el revisor no describió qué entrega"), e.ID)
		}
		// decir que algo está mal sin decir qué cambiar deja al humano
		// con un "no" y nada que hacer con él
		if !e.Funciona && len(e.Cambios) == 0 {
			return Veredicto{}, fmt.Errorf(idioma.T("%s: el revisor la marca rota pero no dice qué cambiar"), e.ID)
		}
	}

	// un informe que se salta tareas haría aprobar código que nadie miró
	var faltan []string
	for _, t := range esperadas {
		if !vistas[t.ID] {
			faltan = append(faltan, t.ID)
		}
	}
	if len(faltan) > 0 {
		return Veredicto{}, fmt.Errorf(idioma.T("el revisor no juzgó %s"), strings.Join(faltan, ", "))
	}
	return v, nil
}

func tipoValido(tipo string) bool {
	for _, t := range TiposCommit {
		if t == tipo {
			return true
		}
	}
	return false
}

// normalizarInforme lleva las llaves en inglés del informe a las canónicas.
func normalizarInforme(data []byte) []byte {
	data = idioma.NormalizarClaves(data, map[string]string{"tasks": "tareas", "notes": "notas"})
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	var items []json.RawMessage
	if json.Unmarshal(m["tareas"], &items) != nil {
		return data
	}
	for i, it := range items {
		items[i] = idioma.NormalizarClaves(it, map[string]string{"type": "tipo", "works": "funciona", "description": "descripcion", "changes": "cambios"})
	}
	m["tareas"], _ = json.Marshal(items)
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}
