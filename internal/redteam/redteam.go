// Package redteam attacks a finished task before it ships: one model call
// writes hostile tests against the signatures the task exposes, and code, not
// the model, decides which failures are real. Only a crash that reproduces
// twice inside the task's own code counts as confirmed; an assertion the
// attacker invented is a claim, and a test that does not even compile is the
// attacker's fault and is thrown away.
package redteam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

type Clase string

const (
	Confirmado    Clase = "confirmed"      // crash/race in the task's code, reproduced twice
	Afirmacion    Clase = "claim"          // an assertion failed; the attacker may be wrong
	FallaAtacante Clase = "attacker_fault" // the test itself is broken
	Resistio      Clase = "held"
)

// MaxAtaques is the most attacks asked from the model per task.
const MaxAtaques = 8

// invariantes the attacker may cite besides spec ids, rules, acceptance and dna.
var clasesRobustez = []string{"nil_edge", "boundary", "forbidden_path", "concurrency", "injection"}

type Ataque struct {
	Nombre     string   `json:"name"`
	Invariante string   `json:"invariant"`
	Objetivo   string   `json:"target"`
	Imports    []string `json:"imports"`
	Test       string   `json:"test"`
}

type Resultado struct {
	Ataque  Ataque `json:"attack"`
	Clase   Clase  `json:"class"`
	Detalle string `json:"detail,omitempty"`
}

type Informe struct {
	Tarea       string      `json:"task"`
	Resultados  []Resultado `json:"results"`
	Descartados []string    `json:"discarded,omitempty"`
	Omitido     string      `json:"skipped,omitempty"`
}

// Confirmados are the attacks that proved a real crash.
func (i Informe) Confirmados() []Resultado {
	var out []Resultado
	for _, r := range i.Resultados {
		if r.Clase == Confirmado {
			out = append(out, r)
		}
	}
	return out
}

// Superficie is the only ground an attack may stand on.
type Superficie struct {
	Requisitos map[string]string // id -> text
	Reglas     []string
	Aceptacion []string
	DNA        string // the active DNA block, or ""
	Diff       string // the task's diff, already capped
	Lenguaje   string // "go" or "python"
}

// Prompt asks for hostile tests, bounded to real invariants.
func Prompt(t task.Task, s Superficie) string {
	var b strings.Builder
	b.WriteString("You are Loom Red Team, a saboteur. A task is finished and about to ship. Write hostile tests that try to break what it exposes. You may only attack invariants that really exist; an attack on an invented requirement is thrown away.\n\n")
	fmt.Fprintf(&b, "TASK %s: %s\nwhy: %s\ndone_when: %s\n", t.ID, t.Titulo, t.Porque, t.ListoCuando)
	b.WriteString("EXPOSED SIGNATURES (the only things your tests may call):\n")
	for _, e := range t.Expone {
		b.WriteString("- " + strings.TrimSpace(e) + "\n")
	}
	if len(t.NoTocar) > 0 {
		b.WriteString("FORBIDDEN PATHS: " + strings.Join(t.NoTocar, ", ") + "\n")
	}
	b.WriteString("\nINVARIANTS YOU MAY ATTACK\n")
	ids := make([]string, 0, len(s.Requisitos))
	for id := range s.Requisitos {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "- spec:%s  %s\n", id, s.Requisitos[id])
	}
	for i, r := range s.Reglas {
		fmt.Fprintf(&b, "- rule:%d  %s\n", i+1, r)
	}
	for i, a := range s.Aceptacion {
		fmt.Fprintf(&b, "- acceptance:%d  %s\n", i+1, a)
	}
	if s.DNA != "" {
		b.WriteString(s.DNA + "\n(cite one as dna:<name>)\n")
	}
	b.WriteString("- robustness classes: " + strings.Join(clasesRobustez, ", ") + " (nil/empty/huge inputs that crash, limit values, paths outside the allowed ones, data races, untrusted strings reaching a shell, query or path)\n")
	if s.Diff != "" {
		b.WriteString("\nTHE TASK'S DIFF (aim at it)\n```diff\n" + s.Diff + "\n```\n")
	}
	fmt.Fprintf(&b, "\nWrite at most %d attacks in %s. Each is ONE test function named TestRedTeam<N> (Python: test_redteam_<n>) that calls an exposed signature with hostile input and asserts the contract holds. Prefer attacks that crash or race: those need no assertion about expected values. No comments in the code.\n\n", MaxAtaques, s.Lenguaje)
	b.WriteString("Return ONLY this JSON, no text around it:\n")
	b.WriteString(`{"attacks": [{"name": "short name", "invariant": "one of the invariants above", "target": "the exposed signature attacked, copied exactly", "imports": ["extra imports the test needs"], "test": "the complete test function"}]}`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// Parse reads the model's attacks and drops, with a reason, every one that
// is not grounded: unknown invariant, a target the task does not expose, or a
// test that never calls the target.
func Parse(texto string, t task.Task, s Superficie) (validos []Ataque, descartados []string, err error) {
	c := plan.CuerpoJSON(texto)
	if c == "" {
		return nil, nil, errors.New(idioma.T("el modelo no devolvió JSON · vuelve a intentarlo"))
	}
	var doc struct {
		Attacks []Ataque `json:"attacks"`
	}
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(c))).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON from the attacker · %w", err)
	}
	for i, a := range doc.Attacks {
		nombre := a.Nombre
		if nombre == "" {
			nombre = fmt.Sprintf("attack %d", i+1)
		}
		switch {
		case strings.TrimSpace(a.Test) == "":
			descartados = append(descartados, nombre+": empty test")
		case !invarianteValido(a.Invariante, s):
			descartados = append(descartados, nombre+": invariant "+fmt.Sprintf("%q", a.Invariante)+" is not in the surface")
		case !exponeEsa(t, a.Objetivo):
			descartados = append(descartados, nombre+": target is not an exposed signature")
		case !strings.Contains(a.Test, simbolo(a.Objetivo)):
			descartados = append(descartados, nombre+": the test never calls its target")
		default:
			if len(validos) < MaxAtaques {
				validos = append(validos, a)
			}
		}
	}
	return validos, descartados, nil
}

func invarianteValido(inv string, s Superficie) bool {
	inv = strings.TrimSpace(inv)
	for _, c := range clasesRobustez {
		if inv == c {
			return true
		}
	}
	tipo, resto, ok := strings.Cut(inv, ":")
	if !ok {
		return false
	}
	switch tipo {
	case "spec":
		_, hay := s.Requisitos[resto]
		return hay
	case "rule":
		return indiceEn(resto, len(s.Reglas))
	case "acceptance":
		return indiceEn(resto, len(s.Aceptacion))
	case "dna":
		return s.DNA != "" && strings.Contains(s.DNA, "- "+resto+":")
	}
	return false
}

func indiceEn(s string, n int) bool {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return err == nil && i >= 1 && i <= n
}

func exponeEsa(t task.Task, objetivo string) bool {
	objetivo = strings.TrimSpace(objetivo)
	for _, e := range t.Expone {
		if strings.TrimSpace(e) == objetivo {
			return true
		}
	}
	return false
}

// simbolo is the last name of a signature: wol.Send(...) -> Send.
func simbolo(firma string) string {
	n := task.NombreDeFirma(firma)
	if i := strings.LastIndexAny(n, "./"); i >= 0 {
		n = n[i+1:]
	}
	return n
}

var reGoFrame = regexp.MustCompile(`(?m)^\s+(/\S+\.go):(\d+)`)
var rePyFrame = regexp.MustCompile(`(?m)^(\S+\.py):(\d+): (\w+)`)

// Clasificar decides, from the runner's output only, what an attack proved.
func Clasificar(salida, lenguaje string, ok bool) (Clase, string) {
	if ok {
		return Resistio, ""
	}
	if lenguaje == "python" {
		return clasificarPython(salida)
	}
	return clasificarGo(salida)
}

func clasificarGo(s string) (Clase, string) {
	switch {
	case strings.Contains(s, "[build failed]") || strings.Contains(s, "[setup failed]") || strings.Contains(s, "build constraints exclude"):
		return FallaAtacante, "the test does not compile"
	case strings.Contains(s, "DATA RACE"):
		if f := primerMarcoPropio(s); f != "" {
			return Confirmado, "data race · " + f
		}
		return FallaAtacante, "race inside the test itself"
	case strings.Contains(s, "panic:") && !strings.Contains(s, "test timed out"):
		f := primerMarcoPropio(s)
		if f == "" {
			return FallaAtacante, "panic with no frame in the task's code"
		}
		return Confirmado, strings.TrimSpace(primeraLinea(s, "panic:")) + " · " + f
	case strings.Contains(s, "--- FAIL") || strings.Contains(s, "test timed out"):
		return Afirmacion, strings.TrimSpace(primeraLinea(s, "--- FAIL"))
	}
	return FallaAtacante, "unrecognized failure"
}

// primerMarcoPropio is the first stack frame in code that is neither the Go
// toolchain nor a test file: where the crash really lives.
func primerMarcoPropio(s string) string {
	for _, m := range reGoFrame.FindAllStringSubmatch(s, -1) {
		f := m[1]
		if strings.HasSuffix(f, "_test.go") || strings.Contains(f, "/runtime/") || strings.Contains(f, "/testing/") ||
			strings.Contains(f, "/go/src/") || strings.Contains(f, "/sync/") || strings.Contains(f, "/usr/lib/go") || strings.Contains(f, "/usr/local/go") {
			continue
		}
		return f + ":" + m[2]
	}
	return ""
}

func clasificarPython(s string) (Clase, string) {
	for _, malo := range []string{"SyntaxError", "ImportError", "ModuleNotFoundError", "NameError", "IndentationError"} {
		if strings.Contains(s, "E   "+malo) || strings.Contains(s, malo+":") {
			return FallaAtacante, "the test does not run: " + malo
		}
	}
	for _, m := range rePyFrame.FindAllStringSubmatch(s, -1) {
		if strings.HasPrefix(m[1], "test_") || strings.Contains(m[1], "/test_") || strings.Contains(m[1], "loom_redteam") {
			continue
		}
		if m[3] == "AssertionError" {
			return Afirmacion, m[1] + ":" + m[2]
		}
		return Confirmado, m[3] + " · " + m[1] + ":" + m[2]
	}
	if strings.Contains(s, "AssertionError") || strings.Contains(s, "FAILED") {
		return Afirmacion, strings.TrimSpace(primeraLinea(s, "FAILED"))
	}
	return FallaAtacante, "unrecognized failure"
}

func primeraLinea(s, marca string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, marca) {
			if len(l) > 200 {
				l = l[:200]
			}
			return l
		}
	}
	return ""
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Generador is the model that writes the attacks.
type Generador interface {
	Generar(ctx context.Context, prompt string) (string, error)
}
