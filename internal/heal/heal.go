// Package heal turns a failure log into a spec change and a corrective
// task: the planner names the invariant the spec lacked, a command that
// reproduces the failure, and the files to fix.
package heal

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

const (
	maxLineas = 200
	maxBytes  = 12000
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Recortar keeps the tail of a log, where the failure usually is: no color
// codes, at most maxLineas lines and maxBytes bytes.
func Recortar(log string) string {
	log = strings.TrimSpace(ansi.ReplaceAllString(log, ""))
	lineas := strings.Split(log, "\n")
	if len(lineas) > maxLineas {
		lineas = lineas[len(lineas)-maxLineas:]
	}
	s := strings.Join(lineas, "\n")
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
	}
	return s
}

// Diagnostico is what the planner returns about a failure.
type Diagnostico struct {
	Titulo        string   `json:"title"`
	Causa         string   `json:"cause"`
	Invariante    string   `json:"invariant"`
	Criterio      string   `json:"criterion"`
	Comando       string   `json:"command"`
	ArchivoPrueba string   `json:"test_file"`
	Tocar         []string `json:"only_touch"`
	Como          string   `json:"how"`
}

// Prompt asks the planner for a diagnosis. spec is the human spec text,
// repo the compact repository map.
func Prompt(log, spec, repo, pruebas string) string {
	var b strings.Builder
	b.WriteString("You are Loom Heal. Software built from a spec failed in the field. Find what the spec was missing and describe the smallest corrective task.\n\n")
	b.WriteString("FAILURE LOG (tail)\n```\n" + log + "\n```\n\n")
	b.WriteString("CURRENT SPEC\n```yaml\n" + spec + "\n```\n\n")
	if repo != "" {
		b.WriteString(repo + "\n\n")
	}
	if pruebas != "" {
		b.WriteString("Project test command: " + pruebas + "\n\n")
	}
	b.WriteString(`Read the failure and decide:
- "invariant": ONE sentence stating the rule or edge case the spec lacked, written so it holds for every future change (a rule, not a bug report).
- "criterion": the acceptance criterion that would have caught it, one sentence.
- "command": an executable command that reproduces the failure and FAILS TODAY: it runs a test file that does not exist yet (named in "test_file"), so it fails until the task writes and passes it. Use the project's own test runner, narrowed to that file.
- "test_file": the path of the new test, in the project's test conventions.
- "only_touch": globs of the source files that most likely need the fix (use paths from the log and the repository map), not the test file.
- "cause": two sentences at most on why it failed.
- "title": short lowercase phrase for the corrective task.
- "how": the fix approach and the exact failing case as input → expected output, so the implementer can write the test from it.

Return ONLY this JSON, no text around it:
{"title": "", "cause": "", "invariant": "", "criterion": "", "command": "", "test_file": "", "only_touch": [""], "how": ""}`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// Parse reads the planner's diagnosis and checks it is actionable.
func Parse(texto string) (Diagnostico, error) {
	t := plan.CuerpoJSON(texto)
	if t == "" {
		return Diagnostico{}, errors.New("el modelo no devolvió un diagnóstico · vuelve a intentarlo")
	}
	var d Diagnostico
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(t))).Decode(&d); err != nil {
		return d, fmt.Errorf("el diagnóstico no es JSON válido · %s", err)
	}
	d.Invariante, d.Comando, d.ArchivoPrueba = strings.TrimSpace(d.Invariante), strings.TrimSpace(d.Comando), strings.TrimSpace(d.ArchivoPrueba)
	switch {
	case d.Invariante == "":
		return d, errors.New("el diagnóstico no nombra la regla que faltaba · vuelve a intentarlo")
	case d.Comando == "":
		return d, errors.New("el diagnóstico no trae un comando que reproduzca el fallo · vuelve a intentarlo")
	case len(d.Tocar) == 0 && d.ArchivoPrueba == "":
		return d, errors.New("el diagnóstico no dice qué archivos tocar · vuelve a intentarlo")
	}
	if strings.TrimSpace(d.Criterio) == "" {
		d.Criterio = d.Invariante
	}
	if strings.TrimSpace(d.Titulo) == "" {
		d.Titulo = "fix: " + d.Invariante
	}
	return d, nil
}

// Tarea builds the corrective contract. The test file joins the scope so
// the task can write the reproduction before fixing.
func Tarea(d Diagnostico, id, log string) task.Task {
	tocar := append([]string{}, d.Tocar...)
	if d.ArchivoPrueba != "" {
		tocar = append(tocar, d.ArchivoPrueba)
	}
	notas := "A failure reached the field and the spec did not cover it. First write the test that reproduces it" +
		testEn(d.ArchivoPrueba) + " so that done_when fails for the right reason, then fix the code until it passes. Do not weaken existing tests.\n" +
		"Cause: " + strings.TrimSpace(d.Causa) + "\n" +
		"Rule the spec now carries: " + d.Invariante + "\n"
	if c := strings.TrimSpace(d.Como); c != "" {
		notas += "Cases: " + c + "\n"
	}
	notas += "\nFailure log (tail):\n```\n" + tail(log, 3000) + "\n```"
	return task.Task{
		Version:        task.Version,
		ID:             id,
		Titulo:         d.Titulo,
		Porque:         d.Invariante,
		ListoCuando:    d.Comando,
		TocarSolo:      tocar,
		Peso:           "media",
		LimiteIntentos: task.DefaultLimiteIntentos,
		LimiteLineas:   0,
		Notas:          notas,
	}
}

func testEn(f string) string {
	if f == "" {
		return ""
	}
	return " in " + f
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
