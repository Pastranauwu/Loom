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
		return Veredicto{}, errors.New("sin revisor · declara el modelo del rol revisor en config.yml")
	}
	if strings.TrimSpace(o.Diff) == "" {
		return Veredicto{}, errors.New("no hay diff que revisar")
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
		return Veredicto{}, fmt.Errorf("el revisor no respondió · %s", err)
	}
	return Parse(texto, o.Tareas)
}

// Prompt arma la instrucción del revisor: qué se pidió y qué se escribió.
func Prompt(tareas []task.Task, diff string) string {
	var b strings.Builder
	b.WriteString("Eres el revisor de loom. Decides si esta entrega entra en la rama principal.\n\n")
	b.WriteString("Ya se verificó por código, no lo repitas: la rama rebasea limpio, cada tarea es un commit, ")
	b.WriteString("la suite del proyecto pasa sobre el conjunto, no hay secretos ni prints de debug, y cada tarea ")
	b.WriteString("cabe en su presupuesto de líneas.\n\n")
	b.WriteString("Lo que TÚ juzgas es lo que ninguna comprobación automática ve: si el código hace lo que la tarea pedía.\n\n")

	b.WriteString("Lo que se pidió:\n")
	for _, t := range tareas {
		fmt.Fprintf(&b, "- %s: %s\n", t.ID, t.Titulo)
		if t.Porque != "" {
			fmt.Fprintf(&b, "  por qué: %s\n", t.Porque)
		}
		fmt.Fprintf(&b, "  se da por hecha cuando pasa: %s\n", t.ListoCuando)
		if len(t.Expone) > 0 {
			fmt.Fprintf(&b, "  debía exponer: %s\n", strings.Join(t.Expone, "; "))
		}
	}

	b.WriteString("\nEl diff completo:\n```diff\n")
	b.WriteString(diff)
	b.WriteString("\n```\n\n")

	b.WriteString("Para CADA tarea di si su implementación funciona y clasifícala.\n\n")
	b.WriteString("- \"tipo\": una de " + strings.Join(TiposCommit, ", ") + ", según lo que ENTREGA de verdad, no según cómo se titula. Empaquetado, Dockerfile o CI son chore; solo documentación es docs; corregir algo roto es fix; funcionalidad nueva es feat.\n")
	b.WriteString("- \"descripcion\": una frase corta y concreta de lo que entrega, para que un humano sepa qué está aprobando sin leerse el diff.\n")
	b.WriteString("- \"funciona\": false SOLO por defectos que puedas señalar en el diff:\n")
	b.WriteString("  · el código no hace lo que su tarea decía, o solo funciona para el caso exacto que prueba el test\n")
	b.WriteString("  · un fallo real: caso límite roto, error sin manejar que pierde datos, condición de carrera\n")
	b.WriteString("  · un problema de seguridad: entrada sin validar en una frontera de confianza, secreto derivado, permiso de más\n")
	b.WriteString("  · lo que entrega no encaja con lo que otra tarea consume\n")
	b.WriteString("  · el diff borra, relaja o reescribe casos de una prueba que ya existía y la tarea no los declara en \"Obsoleto:\": una prueba vieja solo cambia en lo que el contrato dice que dejó de valer\n")
	b.WriteString("- \"cambios\": si funciona es false, qué hay que corregir, una línea por cosa y con archivo:línea. Obligatorio: decir que algo está mal sin decir qué cambiar no le sirve a nadie.\n\n")
	b.WriteString("NO marques funciona:false por estilo, nombres, formato, gustos de arquitectura, ni por falta de pruebas o documentación que nadie pidió. Si dudas y no puedes señalar la línea concreta, es que funciona.\n\n")
	b.WriteString("En \"notas\" pon solo lo que se ve mirando el conjunto y no cabe en ninguna tarea suelta; vacío si no hay nada.\n\n")
	b.WriteString("Devuelve SOLO este JSON, sin texto alrededor, con una entrada por cada tarea de arriba:\n")
	b.WriteString(`{"tareas": [{"id": "T-001", "tipo": "feat", "funciona": true, "descripcion": "qué entrega", "cambios": []}], "notas": ""}`)
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
		return Veredicto{}, errors.New("el revisor no devolvió un informe legible")
	}
	var v Veredicto
	if err := json.Unmarshal([]byte(t[ini:fin+1]), &v); err != nil {
		return Veredicto{}, fmt.Errorf("el informe del revisor no es JSON válido · %s", err)
	}
	if len(v.Tareas) == 0 {
		return Veredicto{}, errors.New("el revisor no juzgó ninguna tarea")
	}

	vistas := map[string]bool{}
	for i := range v.Tareas {
		e := &v.Tareas[i]
		e.ID = strings.TrimSpace(e.ID)
		if e.ID == "" {
			return Veredicto{}, errors.New("el revisor devolvió una entrada sin id de tarea")
		}
		vistas[e.ID] = true
		if !tipoValido(e.Tipo) {
			return Veredicto{}, fmt.Errorf("%s: tipo %q no es de Conventional Commits (%s)",
				e.ID, e.Tipo, strings.Join(TiposCommit, ", "))
		}
		if strings.TrimSpace(e.Descripcion) == "" {
			return Veredicto{}, fmt.Errorf("%s: el revisor no describió qué entrega", e.ID)
		}
		// decir que algo está mal sin decir qué cambiar deja al humano
		// con un "no" y nada que hacer con él
		if !e.Funciona && len(e.Cambios) == 0 {
			return Veredicto{}, fmt.Errorf("%s: el revisor la marca rota pero no dice qué cambiar", e.ID)
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
		return Veredicto{}, fmt.Errorf("el revisor no juzgó %s", strings.Join(faltan, ", "))
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
