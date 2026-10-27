// Package ship implements the exit gate: the deterministic
// steps a task must pass before loom opens a PR. Every check is pure
// code; no model is involved in verification.
//
// Los pasos corren en orden y la compuerta se detiene en el primero que
// falla: sin PR, con la razón exacta.
package ship

import (
	"context"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// DefaultTimeout is the fallback for the bisectable step's test run.
const DefaultTimeout = 5 * time.Minute

// Paso is the result of one exit-gate step, in order.
type Paso struct {
	Nombre  string `json:"nombre"`
	OK      bool   `json:"ok"`
	Detalle string `json:"detalle,omitempty"`
}

// Resultado is the outcome of the exit gate on one task.
type Resultado struct {
	ID       string `json:"id"`
	Pasos    []Paso `json:"pasos"`
	Aprobado bool   `json:"aprobado"`
	PR       string `json:"pr,omitempty"`

	// Resumen para las métricas, aunque la esclusa frene antes.
	LineasMas   int      `json:"lineas_mas,omitempty"`
	LineasMenos int      `json:"lineas_menos,omitempty"`
	Ruido       int      `json:"ruido,omitempty"`
	Conflicto   bool     `json:"conflicto,omitempty"`
	Brecha      *float64 `json:"brecha,omitempty"` // visible_pct - hidden_pct del examen ciego
}

// Opciones carries the exit gate's dependencies.
type Opciones struct {
	Root    string
	Room    room.Room
	Task    task.Task
	Config  config.Config
	Modelo  string        // del último intento, para el trailer Agent:
	Base    string        // rama base sobre la que rebasear
	Timeout time.Duration // timeout del paso bisectable
	DryRun  bool          // corre todo menos abrir el PR
	// Conservar deja la rama del cuarto como estaba al terminar: la
	// esclusa la rebasa y la aplana para revisarla, y un `--dry-run` que
	// se queda con eso ya no es en seco. En una corrida real, dejó la rama de
	// T-035 con el esqueleto del que dependía metido en su commit, y la
	// corrida siguiente no pudo juntarla con él (conflictos agregar/agregar).
	// La entrega conjunta no lo usa: necesita el commit aplanado.
	Conservar bool
	Progreso  func(Paso) // llamado tras cada paso, para el TUI; nil = silencio
	// RedTeam attacks the task before it ships; nil skips the step. It returns
	// the detail and whether the gate may go on.
	RedTeam func(context.Context) (string, bool)
	// SuiteAlIntegrar es la entrega conjunta: la suite completa la corre
	// el paso integradas sobre todo junto y la base actual. En la rama de
	// la tarea, que arrancó de una base vieja, un rojo puede ser algo que
	// la base ya arregló después (un timeout corregido en main
	// frenaba a T-048), así que ahí basta su listo_cuando.
	SuiteAlIntegrar bool
}

// Run executes the nine steps in order and returns the gate result.
// The first failing step stops the gate (la compuerta se frena ahí).
func Run(ctx context.Context, o Opciones) Resultado {
	res := Resultado{ID: o.Task.ID}
	if o.Base == "" {
		o.Base = "HEAD"
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	apuntar := func(p Paso) {
		res.Pasos = append(res.Pasos, p)
		if o.Progreso != nil {
			o.Progreso(p)
		}
	}

	if o.Conservar {
		if punta, err := gitRun(o.Room.Path, "rev-parse", "HEAD"); err == nil {
			defer func() {
				_, _ = gitRun(o.Room.Path, "rebase", "--abort")
				_, _ = gitRun(o.Room.Path, "reset", "--hard", strings.TrimSpace(punta))
			}()
		}
	}

	// 1. base — rebase sobre la rama base
	target, conflictos, err := rebase(ctx, o.Root, o.Room.Path, o.Base, o.Room.Rama, prLocal(o.Root, o.Config))
	if err != nil {
		if len(conflictos) > 0 {
			res.Conflicto = true
			apuntar(Paso{"base", false, idioma.T("rebase en conflicto · archivos: ") + unir(conflictos) + " · resuélvelo a mano"})
		} else {
			apuntar(Paso{"base", false, idioma.T("no se pudo rebasear · ") + err.Error()})
		}
		return res
	}
	apuntar(Paso{"base", true, idioma.T("rebaseado sobre ") + target})

	// 2. historial — aplanar los wip en un commit limpio
	tocados, _ := diffArchivos(o.Room.Path, target)
	tipo := tipoCommit(o.Task.Titulo, tocados, patronesPruebaDe(o.Config))
	cuenta, _, err := aplanar(ctx, o.Room.Path, target, o.Task.ID, o.Task.Titulo, tipo, o.Modelo)
	if err != nil {
		apuntar(Paso{"historial", false, err.Error()})
		return res
	}
	apuntar(Paso{"historial", true, itoa(cuenta) + " guardados → 1 commit"})

	// el diff del commit aplanado alimenta a los escáneres
	diff, archivos, mas, menos, masPrueba, err := diffAplanado(o.Room.Path, target, patronesPruebaDe(o.Config))
	if err != nil {
		apuntar(Paso{"ruido", false, err.Error()})
		return res
	}
	res.LineasMas = mas
	res.LineasMenos = menos

	// 3. ruido — prints de debug, temporales, código comentado, comentarios para humanos
	// solo el contrato del esqueleto avisa: el esqueleto lo escribe a
	// propósito y el implementador debe borrarlo. El resto frena.
	var frena, avisa []Hallazgo
	for _, x := range escanearRuido(diff, archivos) {
		if x.Tipo == "contrato del esqueleto" {
			avisa = append(avisa, x)
		} else {
			frena = append(frena, x)
		}
	}
	res.Ruido = len(frena) + len(avisa)
	if len(frena) > 0 {
		apuntar(Paso{"ruido", false, resumenHallazgos(frena)})
		return res
	}
	if len(avisa) > 0 {
		apuntar(Paso{"ruido", true, idioma.T("revísalo a ojo: ") + resumenHallazgos(avisa)})
	} else {
		apuntar(Paso{"ruido", true, idioma.T("sin ruido")})
	}

	// 4. secretos — en el diff y en el commit
	if h := escanearSecretos(diff); len(h) > 0 {
		apuntar(Paso{"secretos", false, resumenHallazgos(h)})
		return res
	}
	apuntar(Paso{"secretos", true, idioma.T("sin secretos")})

	// 5. presupuesto — limite_lineas y archivos tocados
	if detalle, ok := verificarPresupuesto(mas, menos, masPrueba, len(archivos), o.Task); !ok {
		apuntar(Paso{"presupuesto", false, detalle})
		return res
	} else {
		apuntar(Paso{"presupuesto", true, detalle})
	}

	// 6. interfaces — entregó lo que sus hermanas consumen
	faltan, sinForma := verificarExpone(o.Task.Expone, diff)
	if len(faltan) > 0 {
		apuntar(Paso{"interfaces", false, idioma.T("no expone lo prometido: ") + strings.Join(faltan, "; ")})
		return res
	}
	detalleIfaces := "expone lo prometido"
	if len(sinForma) > 0 {
		detalleIfaces = "sin verificar (no son firmas, revísalo a ojo): " + strings.Join(sinForma, "; ")
	}
	apuntar(Paso{"interfaces", true, detalleIfaces})

	// 6.5 dependencias — verifica el grafo de imports del diff
	if detalle, ok := verificarDependencias(diff, o.Config.ReglasImport); !ok {
		apuntar(Paso{"dependencias", false, detalle})
		return res
	} else if len(o.Config.ReglasImport) > 0 {
		apuntar(Paso{"dependencias", true, detalle})
	}

	// 6.6 dna — lo que del ADN del proyecto se puede comprobar en el diff
	if len(o.Config.DNA) > 0 {
		detalle, ok := verificarDNA(o.Config.DNA, diff, o.Config.ReglasImport)
		apuntar(Paso{"dna", ok, detalle})
		if !ok {
			return res
		}
	}

	// 6.7 estricto — lo que deja pasar al compilador: ts-ignore, any, unsafe, Python sin tipos
	if aplicaEstricto(archivos) {
		if h := escanearEstricto(diff, o.Task); len(h) > 0 {
			apuntar(Paso{"estricto", false, resumenHallazgos(h)})
			return res
		}
		apuntar(Paso{"estricto", true, idioma.T("sin código permisivo")})
	}

	// 7. bisectable — el commit compila y pasa las pruebas
	detalle, ok := bisectable(ctx, o)
	if !ok {
		apuntar(Paso{"bisectable", false, detalle})
		return res
	}
	apuntar(Paso{"bisectable", true, detalle})

	// 7.5 redteam — a saboteur tries to break what the task exposes
	if o.RedTeam != nil {
		detalle, ok := o.RedTeam(ctx)
		apuntar(Paso{"redteam", ok, detalle})
		if !ok {
			return res
		}
	}

	// 8. suite_oculta — hidden test gate; skipped if no sealed suite
	pruebas := o.Config.Pruebas
	if pruebas == "" {
		pruebas = o.Task.ListoCuando
	}
	if brecha, detalle, suiteOK := verificarSuiteOculta(ctx, o.Root, o.Room.Path, o.Task, pruebas, o.Timeout); detalle != "" {
		res.Brecha = brecha
		if suiteOK {
			apuntar(Paso{"suite_oculta", true, detalle})
		} else {
			apuntar(Paso{"suite_oculta", false, detalle})
			return res
		}
	}

	// 9. handoff — qué cambió, qué no, cómo verificar
	cuerpo := generarHandoff(o.Task, archivos, mas, menos)
	apuntar(Paso{"handoff", true, ""})

	// 10. pr — abrir y liberar el cuarto
	if o.DryRun {
		apuntar(Paso{"pr", true, idioma.T("dry-run · sin PR")})
		res.Aprobado = true
		return res
	}
	var url string
	if prLocal(o.Root, o.Config) {
		// la rama se queda con su cuarto: es lo que se revisa y se mergea
		url, err = abrirPRLocal(o.Root, o.Room.Rama, o.Base, o.Task.Titulo, cuerpo)
	} else {
		url, err = abrirPR(ctx, o.Root, o.Room, o.Base, o.Task.Titulo, cuerpo)
	}
	if err != nil {
		apuntar(Paso{"pr", false, err.Error()})
		return res
	}
	apuntar(Paso{"pr", true, url})
	res.PR = url
	res.Aprobado = true
	return res
}

// PrimerMotivo returns the detail of the first failed step.
func (r Resultado) PrimerMotivo() string {
	for _, p := range r.Pasos {
		if !p.OK {
			return p.Detalle
		}
	}
	return ""
}

// bisectable exige la suite del proyecto sobre el commit de la tarea, y
// se conforma con su listo_cuando cuando la suite roja no es de ella: en
// la entrega conjunta (SuiteAlIntegrar) o si ya fallaba al empezar la
// tarea (con esqueleto, la suite sigue roja hasta rellenar todo). En los
// dos casos la suite completa la exige el paso integradas.
func bisectable(ctx context.Context, o Opciones) (string, bool) {
	detalle, ok := verificarBisectable(ctx, o.Room.Path, o.Config.Pruebas, o.Timeout)
	if ok {
		return detalle, true
	}
	switch {
	case o.SuiteAlIntegrar:
		detalle, ok = verificarBisectable(ctx, o.Room.Path, o.Task.ListoCuando, o.Timeout)
		return "la suite no pasa en la rama de la tarea · " + detalle + " · la suite completa se exige al integrar, sobre la base actual", ok
	case suiteYaFallaba(ctx, o.Room.Path, o.Base, o.Config.Pruebas, o.Timeout):
		detalle, ok = verificarBisectable(ctx, o.Room.Path, o.Task.ListoCuando, o.Timeout)
		return "la suite ya fallaba al empezar la tarea · " + detalle + " · la suite completa se exige al integrar", ok
	}
	return detalle, false
}
