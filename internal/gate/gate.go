// Package gate implements the entry gate: the checks a
// task must pass before loom spends a single token on it.
//
//  0. el contrato es válido (version, id, límites)
//  1. listo_cuando existe y se ejecuta
//  2. el comando falla hoy (si ya pasa, la tarea no tiene sentido)
//  3. tocar_solo se declara si hace falta y no se cruza con el de otra
//  4. tocar_solo no incluye zonas prohibidas globales ni rutas de prueba
//
// All checks are pure code: no model is involved in verification.
package gate

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

// DefaultTimeout is the maximum time check 2 gives listo_cuando to run.
const DefaultTimeout = 5 * time.Minute

// Check is the result of one gate check.
type Check struct {
	Nombre string `json:"nombre"`
	OK     bool   `json:"ok"`
	Motivo string `json:"motivo,omitempty"`
}

// Result is the outcome of running the gate on one task.
type Result struct {
	ID       string  `json:"id"`
	Aprobada bool    `json:"aprobada"`
	Chequeos []Check `json:"chequeos"`
	Aviso    string  `json:"aviso,omitempty"`
}

// Cruce runs only the scope check between tasks:
// rejects when tocar_solo overlaps another's, or is empty while another
// task runs. cmd/run uses it at assignment time, where the tasks already
// passed the full gate, so no test command runs again.
func Cruce(t task.Task, otras []task.Task) Check {
	return checkSinCruce(t, otras)
}

// PrimerMotivo returns the reason of the first failed check.
func (r Result) PrimerMotivo() string {
	for _, c := range r.Chequeos {
		if !c.OK {
			return c.Motivo
		}
	}
	return ""
}

// Run executes the five checks in order. Check 2 runs listo_cuando for
// real, with timeout, in the repository root. otras carries only the
// tasks already en_curso, which is what decides whether tocar_solo may
// stay empty.
func Run(ctx context.Context, root string, cfg config.Config, t task.Task, otras []task.Task, timeout time.Duration) Result {
	res := Result{ID: t.ID}
	res.Aviso = t.Aviso

	// el contrato se valida primero: sin version ni id no hay tarea
	// que evaluar. Los demás chequeos corren igual, para
	// que un solo paso liste todo lo que hay que arreglar.
	res.Chequeos = append(res.Chequeos, checkContrato(t))

	ejecutable := checkEjecutable(root, t)
	res.Chequeos = append(res.Chequeos, ejecutable)
	if ejecutable.OK {
		res.Chequeos = append(res.Chequeos, checkFallaHoy(ctx, root, t.ListoCuando, timeout, t.Red))
	} else {
		res.Chequeos = append(res.Chequeos, Check{
			Nombre: "falla hoy",
			OK:     false,
			Motivo: "no evaluado · listo_cuando no es ejecutable",
		})
	}
	res.Chequeos = append(res.Chequeos, checkSinCruce(t, otras))
	res.Chequeos = append(res.Chequeos, checkZonasProhibidas(t, cfg.ZonasProhibidas))
	res.Chequeos = append(res.Chequeos, checkRutasDePrueba(t, cfg.PatronesPrueba))
	res.Chequeos = append(res.Chequeos, checkAgente(t, cfg))

	res.Aprobada = true
	for _, c := range res.Chequeos {
		if !c.OK {
			res.Aprobada = false
			break
		}
	}
	return res
}

// checkContrato: the contract itself must be valid before any of the
// runtime checks mean anything.
func checkContrato(t task.Task) Check {
	errs := t.Validate()
	if len(errs) == 0 {
		return Check{"contrato válido", true, ""}
	}
	motivos := make([]string, len(errs))
	for i, e := range errs {
		motivos[i] = e.Error()
	}
	return Check{"contrato válido", false, strings.Join(motivos, " · ")}
}

// checkEjecutable: listo_cuando existe y cada programa que nombra se
// encuentra. Se lee como lo corre el bucle, con `sh -c`: el primer
// programa de cada comando simple, sin los builtins del shell. Mirar solo
// el primer token rechazaba `cd backend && pytest` —el plan de un
// monorepo— con "comando no encontrado: cd", y a la vez dejaba pasar un
// pytest inexistente después del &&.
func checkEjecutable(root string, t task.Task) Check {
	cmdStr := strings.TrimSpace(t.ListoCuando)
	if cmdStr == "" {
		return Check{"ejecutable", false, "falta listo_cuando · escribe el comando que dice \"ya está\""}
	}
	dirConocido := true
	for _, bin := range programas(cmdStr) {
		if builtinShell[bin] {
			if bin == "cd" {
				// desde aquí una ruta relativa se resuelve contra un
				// directorio que quizá la tarea todavía no creó
				dirConocido = false
			}
			continue
		}
		if strings.ContainsRune(bin, '/') {
			if !dirConocido {
				continue
			}
			abs := bin
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(root, bin)
			}
			info, err := os.Stat(abs)
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				return Check{"ejecutable", false, fmt.Sprintf(idioma.T("comando no encontrado: %s · instálalo o corrige listo_cuando"), bin)}
			}
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			return Check{"ejecutable", false, fmt.Sprintf(idioma.T("comando no encontrado: %s · instálalo o corrige listo_cuando"), bin)}
		}
	}
	return Check{"ejecutable", true, ""}
}

// builtinShell son los builtins y palabras clave de sh que un listo_cuando
// usa: no son binarios del PATH y LookPath nunca los encuentra.
var builtinShell = map[string]bool{
	"cd": true, "export": true, "source": true, ".": true, "set": true, "unset": true,
	"test": true, "[": true, "true": true, "false": true, ":": true, "exit": true,
	"eval": true, "exec": true, "if": true, "then": true, "else": true, "fi": true,
	"for": true, "while": true, "do": true, "done": true, "!": true,
}

// programas devuelve el primer programa de cada comando simple de cmd:
// parte en &&, ||, ;, | y & fuera de comillas (no en 2>&1 ni &>) y
// salta las asignaciones VAR=valor del inicio.
// ponytail: sin subshells ni $(...); un programa dentro de ellos no se
// revisa, y falla hoy lo corre igual.
func programas(cmd string) []string {
	var segmentos []string
	var actual strings.Builder
	var comilla rune
	rs := []rune(cmd)
	for i, r := range rs {
		redireccion := r == '&' && ((i > 0 && (rs[i-1] == '>' || rs[i-1] == '<')) || (i+1 < len(rs) && rs[i+1] == '>'))
		switch {
		case comilla != 0:
			if r == comilla {
				comilla = 0
			}
		case r == '\'' || r == '"':
			comilla = r
		case (r == '&' && !redireccion) || r == '|' || r == ';' || r == '\n':
			segmentos = append(segmentos, actual.String())
			actual.Reset()
			continue
		}
		actual.WriteRune(r)
	}
	segmentos = append(segmentos, actual.String())

	var out []string
	for _, seg := range segmentos {
		for _, f := range strings.Fields(seg) {
			if eq := strings.IndexByte(f, '='); eq > 0 && !strings.ContainsAny(f[:eq], "/.-") {
				continue // VAR=valor antes del programa
			}
			f = strings.TrimLeft(f, "({")
			if f != "" && f[0] != '$' {
				out = append(out, f)
			}
			break
		}
	}
	return out
}

// checkFallaHoy: the command must fail today. It runs for real with a
// timeout; a zero exit rejects the task as pointless.
func checkFallaHoy(ctx context.Context, root, listoCuando string, timeout time.Duration, red bool) Check {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := task.Shell(ctx, root, listoCuando, red)

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Check{"falla hoy", false, fmt.Sprintf(idioma.T("listo_cuando tardó más de %s · acota el comando"), timeout)}
	}
	if err == nil {
		// vitest y jest ignoran un archivo que no existe si otro de la
		// lista sí: "run nuevo.test.ts viejo.test.ts" corre el viejo y
		// sale verde. La prueba de la tarea todavía no está escrita, así
		// que no es cierto que ya pase (T-036 y T-039
		// rechazadas, y con ellas todo lo que dependía)
		if f := task.PruebaSinEscribir(root, listoCuando); f != "" {
			return Check{"falla hoy", true, "verde solo porque " + f + " todavía no existe"}
		}
		return Check{"falla hoy", false, "listo_cuando ya pasa · nada que hacer: si el cambio es nuevo, escribe primero la prueba que lo exige (que falle hoy) y ponla en listo_cuando; si ya estaba hecho, borra la tarea con loom task rm"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return Check{"falla hoy", true, ""}
	}
	return Check{"falla hoy", false, fmt.Sprintf(idioma.T("no se pudo ejecutar listo_cuando: %v"), err)}
}

// checkSinCruce: tocar_solo must not overlap with any other active
// task's. The caller passes only tasks in state en_curso.
func checkSinCruce(t task.Task, otras []task.Task) Check {
	// sin alcance declarado no hay cruce que detectar, así que
	// vacío solo vale mientras esta sea la única tarea en curso
	if len(t.TocarSolo) == 0 {
		for _, o := range otras {
			if o.ID != t.ID {
				return Check{"sin cruce", false, "tocar_solo obligatorio con más de una tarea activa · declara tus rutas"}
			}
		}
		return Check{"sin cruce", true, ""}
	}
	for _, o := range otras {
		if o.ID == t.ID {
			continue
		}
		for _, a := range t.TocarSolo {
			for _, b := range o.TocarSolo {
				if globsOverlap(a, b) {
					return Check{"sin cruce", false, fmt.Sprintf(idioma.T("tocar_solo se cruza con %s en %s · ajusta los alcances"), o.ID, a)}
				}
			}
		}
	}
	return Check{"sin cruce", true, ""}
}

// AlcanceProhibido reporta si una ruta de tocar_solo cae en una zona
// prohibida global (lockfiles, migraciones, CI, changelog) o en
// una ruta de prueba (las escribe el examinador ciego), y
// devuelve cuál. `plan` la usa para recortar el alcance antes de
// escribir el contrato: el planificador es un modelo y a veces las
// incluye, y un rechazo en la esclusa no tiene arreglo salvo editar a
// mano, que es justo lo que loom evita.
func AlcanceProhibido(p string, zonas, patronesPrueba []string) (string, bool) {
	for _, z := range zonas {
		if globsOverlap(p, z) {
			return z, true
		}
	}
	// nil y vacío NO son lo mismo: nil es "config.yml no los declaró, usa
	// los del proyecto"; un slice vacío es "este stack no tiene
	// examinador ciego, así que las pruebas las escribe quien implementa
	// y ninguna ruta está vedada".
	if patronesPrueba == nil {
		patronesPrueba = config.DefaultTestPatterns()
	}
	for _, patron := range patronesPrueba {
		if esRutaDePrueba(p, patron) {
			return patron, true
		}
	}
	return "", false
}

// checkZonasProhibidas: tocar_solo must not reach the global forbidden
// zones: lockfiles, migrations, CI, changelog.
func checkZonasProhibidas(t task.Task, zonas []string) Check {
	for _, z := range zonas {
		for _, p := range t.TocarSolo {
			if globsOverlap(p, z) {
				return Check{"zonas prohibidas", false, fmt.Sprintf(idioma.T("tocar_solo incluye la zona prohibida %s · quita %s o ajusta el alcance"), z, p)}
			}
		}
	}
	return Check{"zonas prohibidas", true, ""}
}

// checkRutasDePrueba: tocar_solo must never reach the test files.
// Las escribe un examinador ciego; si el
// implementador puede editarlas, el examen no vale nada.
func checkRutasDePrueba(t task.Task, patrones []string) Check {
	// ver AlcanceProhibido: vacío significa "ninguna vedada", no "usa los
	// del proyecto"
	if patrones == nil {
		patrones = config.DefaultTestPatterns()
	}
	for _, patron := range patrones {
		for _, p := range t.TocarSolo {
			if esRutaDePrueba(p, patron) {
				return Check{"sin rutas de prueba", false, fmt.Sprintf(idioma.T("tocar_solo incluye rutas de prueba (%s casa con %s) · las pruebas no las escribe quien implementa"), p, patron)}
			}
		}
	}
	return Check{"sin rutas de prueba", true, ""}
}

// esRutaDePrueba reports whether the tocar_solo entry p aims at test
// files. Deliberately narrower than globsOverlap: `src/export/**`
// contains test files but does not declare them, and rejecting it would
// reject every sane contract. Lo que sí se rechaza es apuntarles:
// `src/export/*_test.go` o cualquier ruta bajo `test/**`.
func esRutaDePrueba(p, patron string) bool {
	if dir := dirPrefix(patron); dir != "" {
		return within(dir, p) || within(dir, path.Base(p))
	}
	if ok, _ := path.Match(patron, path.Base(p)); ok {
		return true
	}
	return false
}

// globsOverlap reports whether two patterns can match the same file.
// Conservative: when in doubt, they overlap.
func globsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	if dir := dirPrefix(a); dir != "" && within(dir, b) {
		return true
	}
	if dir := dirPrefix(b); dir != "" && within(dir, a) {
		return true
	}
	if ok, _ := path.Match(a, b); ok {
		return true
	}
	if ok, _ := path.Match(b, a); ok {
		return true
	}
	// cabeza y cola literales tienen que ser compatibles las dos: con solo
	// la cabeza, "**/*.tsx" (cabeza vacía) se cruzaba con todo, incluso
	// con package-lock.json
	ha, hb := literalHead(a), literalHead(b)
	ta, tb := literalTail(a), literalTail(b)
	return (strings.HasPrefix(ha, hb) || strings.HasPrefix(hb, ha)) &&
		(strings.HasSuffix(ta, tb) || strings.HasSuffix(tb, ta))
}

// literalTail returns the pattern after its last wildcard.
func literalTail(p string) string {
	return p[strings.LastIndexAny(p, "*?]")+1:]
}

// dirPrefix returns the directory a pattern covers when it ends in
// /** or /*, or empty otherwise.
func dirPrefix(p string) string {
	if strings.HasSuffix(p, "/**") {
		return strings.TrimSuffix(p, "/**")
	}
	if strings.HasSuffix(p, "/*") {
		return strings.TrimSuffix(p, "/*")
	}
	return ""
}

// within reports whether pattern p points inside directory dir.
func within(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// literalHead returns the pattern up to its first wildcard.
func literalHead(p string) string {
	i := strings.IndexAny(p, "*?[")
	if i == -1 {
		return p
	}
	return p[:i]
}

// checkAgente verifica que el agente asignado a la tarea (si existe)
// sea conocido (declarado en config.yml o arquetipo predefinido estándar).
func checkAgente(t task.Task, cfg config.Config) Check {
	if t.Agente == "" {
		return Check{"agente conocido", true, ""}
	}
	if _, ok := cfg.ObtenerAgente(t.Agente); ok {
		return Check{"agente conocido", true, ""}
	}
	return Check{
		Nombre: "agente conocido",
		OK:     false,
		Motivo: fmt.Sprintf(idioma.T("agente desconocido: %s · decláralo en config.yml o usa uno existente (backend, frontend, architect, tester, refactor, ejecutor)"), t.Agente),
	}
}

// GlobsSeCruzan reporta si dos globs de tocar_solo pueden alcanzar el
// mismo archivo. `plan` lo usa para avisar de un cruce con una tarea en
// curso ANTES de escribir el contrato, en vez de dejar que la esclusa lo
// rechace con los tokens ya gastados.
func GlobsSeCruzan(a, b string) bool { return globsOverlap(a, b) }
