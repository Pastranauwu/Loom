package loop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/task"
)

// DefaultTimeout is the fallback for PruebaTimeout when Options no lo fija.
const DefaultTimeout = 5 * time.Minute

// DefaultAgentTimeout is the fallback for AgentTimeout. Más largo que
// DefaultTimeout a propósito: una invocación real de agente (edición
// multi-archivo, no solo correr pruebas) tarda más que una corrida de
// pruebas, y cortarla a los 5 minutos quemaba el intento antes de que el
// agente terminara — causa habitual de "se agotaron los intentos" sin PR.
const DefaultAgentTimeout = 20 * time.Minute

// Agent es lo mínimo que el bucle pide al ejecutor: una invocación por
// intento y el gasto de tokens que produjo. La interfaz se declara aquí,
// del lado del consumidor; los adaptadores de internal/executor la
// satisfacen con un shim en cmd/run, sin que el bucle dependa de un
// paquete en obra.
type Agent interface {
	Name() string
	Run(ctx context.Context, req Request) (Result, error)
}

// ErrDetener es un error que el agente devuelve cuando agotó sus propios
// caminos y reintentar no cambiaría nada. Lo produce la recursión
// (internal/recurse) cuando una subtarea quedó roja tras escalar de
// modelo y consultar al orquestador: hacer que el bucle del padre repita
// la descomposición entera quemaría los intentos restantes contra la
// misma pared. El bucle lo trata como detenida inmediata, sin correr
// listo_cuando ni gastar un intento más.
type ErrDetener struct{ Motivo string }

func (e *ErrDetener) Error() string { return e.Motivo }

// MotivoPresupuesto es el motivo con que se detiene una tarea cuando la
// corrida quemó su tope de tokens (`presupuesto_tokens` en config.yml).
// Es una constante para que la recursión y la interfaz lo reconozcan.
const MotivoPresupuesto = "presupuesto de tokens agotado · sube presupuesto_tokens en config.yml o dejalo en 0 para correr sin tope"

// MotivoVentanas es el motivo con que se detiene una tarea cuando su
// gasto pasaría un tope de una ventana rodante (5h/semanal/mensual) del
// proveedor (`presupuesto:` en config.yml). El ledger es global de la
// cuenta, así que vale entre proyectos.
const MotivoVentanas = "ventana de presupuesto agotada · revisa loom usage · sube el tope en el bloque presupuesto: de config.yml"

// Examinador genera la suite de pruebas ciega antes de que el
// implementador empiece. La interfaz vive en loop para evitar
// un ciclo de importación con el paquete examiner.
type Examinador interface {
	Run(ctx context.Context, roomPath string) (bool, error)
}

// Revisor juzga la implementación verde en tests contra su contrato
// (rol `revisor`). Tests verdes no implican contrato cumplido: con
// modelos livianos es habitual que pasen el examen sin hacer lo que
// decía la tarea (happy path, caso exacto del test). Un revisor que
// pide cambios deja la tarea roja y su veredicto entra como contexto
// del intento siguiente — es revisión temprana por tarea, no al final.
// TopeVisual es cuántas veces la revisión visual puede devolver una
// tarea. Después se acepta lo que haya: el gusto de un modelo no debe
// quemar todos los intentos.
const TopeVisual = 2

// RevisorVisual levanta la interfaz del cuarto, la captura y juzga si
// cumple lo que pide la tarea. Degrada en abierto: sin capturas aprueba.
type RevisorVisual interface {
	Revisar(ctx context.Context, cuarto room.Room, tarea task.Task, intento int) (aprobada bool, cambios string, tokens Tokens)
}

type Revisor interface {
	// Revisar devuelve si la tarea puede darse por cumplida y, si no,
	// qué corregir. El diff es el trabajo acumulado desde la base.
	Revisar(ctx context.Context, roomPath string, tarea task.Task, diff string, intento int) (aprobada bool, cambios string, tokens Tokens)
}

// Request es una invocación del agente dentro de su cuarto.
type Request struct {
	RoomPath     string
	Prompt       string
	AllowedGlobs []string
	Model        string
	Timeout      time.Duration
	Env          []string
	// Texto pide un agente sin herramientas que solo responde (el
	// examinador); el implementador las necesita.
	Texto bool
}

// Result es lo que devolvió una invocación.
type Result struct {
	Stdout   string
	Stderr   string // diagnóstico del CLI: por qué no llegó al modelo
	Text     string // respuesta textual del agente (campo "result" del JSON de claude)
	ExitCode int
	Tokens   Tokens
}

// Options lleva las dependencias del bucle.
type Options struct {
	Agent          Agent
	Root           string
	Room           room.Room
	Task           task.Task
	Model          string
	Base           string        // rama base para el diff de símbolos
	PatronesPrueba []string      // rutas de prueba que nunca se editan
	AgentTimeout   time.Duration // timeout de cada invocación del agente
	PruebaTimeout  time.Duration // timeout de cada listo_cuando
	Env            []string      // variables propias del cuarto (PORT, ...)

	// Interfaces son las firmas congeladas de las tareas hermanas que
	// esta consume, ya resueltas por el que llama. Sin esto el
	// agente inventa la firma: las tareas de una misma oleada corren en
	// cuartos separados y no pueden leerse entre sí.
	Interfaces []string

	// Constitucion es el contenido de .loom/constitution.md,
	// ya cargado por quien llama. Si está vacío, no se inyecta nada.
	Constitucion string

	// Skills son las habilidades del rol asignado, solo
	// nombres — etiquetas descriptivas en el prompt.
	Skills []string

	// SkillsContenido es el cuerpo completo de los paquetes de skill real
	// (internal/skills) resueltos para este rol, ya cargado por quien
	// llama. Vacío si no hay ninguno instalado.
	SkillsContenido string

	// OnIntento, si no es nil, avisa cuándo empieza y termina cada
	// intento. Es la única señal de vida en modo plano: una invocación
	// de agente puede tardar veinte minutos y hasta ahora no imprimía
	// nada hasta el final de la corrida entera.
	OnIntento func(intento int, fase string)

	// Examinador, si no es nil, se invoca una vez antes del bucle del
	// implementador para escribir la suite visible y sellar la oculta
	// Si falla, el bucle continúa sin pruebas ciegas.
	Examinador Examinador

	// Revisor, si no es nil, juzga cada intento verde en tests contra
	// el contrato. Si pide cambios, el intento queda rojo y su veredicto
	// se inyecta como contexto del siguiente. Su gasto cuenta igual que
	// el del agente (presupuesto y ventanas).
	Revisor Revisor

	// RevisorVisual, si no es nil, mira capturas de la interfaz después
	// de que el código pasó pruebas y revisor, en las tareas que tocan
	// archivos de interfaz. Como mucho TopeVisual críticas por tarea.
	RevisorVisual RevisorVisual

	// Presupuesto, si no es nil, recibe el gasto de cada intento. Devuelve
	// false cuando la corrida ya quemó su tope: el bucle se detiene con el
	// motivo en vez de gastar el intento siguiente. Solo los bucles hoja
	// lo reciben — el padre recursivo ya lo ve por sus subtareas y no
	// cuenta doble el agregado.
	Presupuesto interface{ Gastar(int) bool }

	// Proveedor es el proveedor del ejecutor (opencode | claude), para
	// atribuir el gasto al ledger de ventanas.
	Proveedor string
	// Ventanas registra el gasto de cada intento en el ledger de ventanas
	// rodantes (internal/ventanas), verde o rojo por igual. Devuelve false
	// cuando ese gasto pasaría un tope configurado para el proveedor: el
	// bucle se detiene con el motivo. Solo los bucles hoja lo reciben.
	Ventanas interface{ Registrar(string, int) bool }
}

// Outcome es el resultado de correr el bucle sobre una tarea.
type Outcome struct {
	Verde       bool   `json:"verde"`
	Intentos    int    `json:"intentos"`
	UltimoError string `json:"ultimo_error,omitempty"`
	Pregunta    string `json:"pregunta,omitempty"`
	// NoEscalar marca un rojo que un modelo más caro no arregla: la
	// prueba ni llegó a correr, el agente solo tocó lo que la reversión
	// de alcance quita, o la tarea ya gastó su tope. En una corrida real, la
	// escalera a qwen-max pagó el 95% de la corrida contra una prueba
	// que no existía.
	NoEscalar bool `json:"no_escalar,omitempty"`
}

// Run dirige el bucle de intentos e instrumenta cada uno en attempts.jsonl.
// Devuelve el Outcome; solo error si el mecanismo falla (no si la tarea
// se agotó: detenerse es un resultado válido, no un fallo).
func Run(ctx context.Context, o Options) (Outcome, error) {
	if o.Agent == nil {
		return Outcome{}, errors.New("bucle sin ejecutor · elige uno con loom doctor")
	}
	if o.Room.Path == "" {
		return Outcome{}, errors.New("bucle sin cuarto · crea el worktree antes de correr")
	}
	if o.AgentTimeout <= 0 {
		o.AgentTimeout = DefaultAgentTimeout
	}
	if o.PruebaTimeout <= 0 {
		o.PruebaTimeout = DefaultTimeout
	}
	// el .venv del cuarto va delante en el PATH del agente y de la prueba
	o.Env = append(append([]string(nil), o.Env...), room.Entorno(o.Room.Path)...)
	// la prueba que listo_cuando nombra y ya estaba cuando arrancó la
	// tarea es el oráculo que alguien más escribió (el esqueleto, el
	// humano): se protege como ruta de prueba y no se examina encima.
	if fijas := pruebasFijas(o.Room, o.Base, o.Task); len(fijas) > 0 {
		o.Examinador = nil
		o.PatronesPrueba = append(append([]string{}, o.PatronesPrueba...), fijas...)
	}
	if len(o.PatronesPrueba) == 0 {
		o.Task.TocarSolo = conPruebasPropias(o.Task)
	}
	if o.PatronesPrueba == nil {
		o.PatronesPrueba = config.DefaultTestPatterns()
	}
	limite := o.Task.LimiteIntentos
	if limite < 1 {
		limite = task.DefaultLimiteIntentos
	}

	base := o.Base
	if base == "" {
		base = "HEAD"
	}
	if hash, err := resolveCommit(o.Room.Path, base); err == nil {
		base = hash
	}

	s, err := openStore(o.Root, o.Task.ID)
	if err != nil {
		return Outcome{}, err
	}
	// escalar de modelo o retomar vuelve a llamar a Run: el límite es por
	// llamada, pero el número que se registra sigue la cuenta de la tarea
	// para que un intento nuevo no pise el intento-N.log de otro
	previos, _ := ReadAttempts(o.Root, o.Task.ID)
	// las críticas visuales ya recibidas cuentan contra el tope aunque se
	// haya escalado o retomado: un modelo exigente no quema intentos
	rechazosVisuales := 0
	for _, p := range previos {
		if p.Revision != nil && p.Revision.Visual != "" {
			rechazosVisuales++
		}
	}

	// tope por tarea, no por llamada: escalar, retomar y --reintentar
	// vuelven a llamar a Run, y un límite de 3 terminó en 15 intentos.
	// Editar el contrato lo reinicia: cuenta desde la última edición.
	// Tampoco cuentan los intentos contra otra base: cuando lo roto
	// estaba ahí, arreglarla y reintentar es lo que se le pidió al humano.
	partida := o.Room.Commit
	if partida == "" {
		partida = base
	}
	arbolBase, _ := gitRun(o.Room.Path, "rev-parse", "--verify", "--quiet", partida+"^{tree}")
	arbolBase = strings.TrimSpace(arbolBase)
	tope := TopeIntentos * limite
	if usados := intentosPagados(previos, contratoEditado(o.Root, o.Task.ID), arbolBase); usados >= tope {
		motivo := fmt.Sprintf("tope de %d intentos agotado · edita %s (listo_cuando, tocar_solo o notas) y reintenta", tope, o.Task.ID)
		return Outcome{Intentos: 0, UltimoError: motivo, Pregunta: motivo, NoEscalar: true}, nil
	} else if tope-usados < limite {
		limite = tope - usados
	}

	// el latido es el único estado en vivo: attempts.jsonl no se escribe
	// hasta que el intento termina, y un intento puede durar veinte
	// minutos. Se borra al salir, pase lo que pase.
	var acumulado Tokens
	var latMu sync.Mutex
	var latActual Latido
	avisar := func(intento int, fase string) {
		l := Latido{
			ID: o.Task.ID, Intento: intento, Limite: limite, Fase: fase,
			Modelo: o.Model, DesdeFase: time.Now().UTC(), Tokens: acumulado,
		}
		latMu.Lock()
		latActual = l
		latMu.Unlock()
		EscribirLatido(o.Root, l)
		if o.OnIntento != nil {
			o.OnIntento(intento, fase)
		}
	}
	defer BorrarLatido(o.Root, o.Task.ID)

	// Latir dentro de la fase, no solo al cambiarla. Una fase `agente`
	// dura lo que dure la invocación; sin refresco el latido se vuelve
	// rancio solo y la tarea viva se vería como interrumpida. El defer va
	// DESPUÉS del de BorrarLatido para correr antes que él (LIFO): si se
	// borrara primero, el latidor podría recrear el archivo de una tarea
	// que ya terminó y dejarla en curso para siempre.
	pararLatido := make(chan struct{})
	var latiendo sync.WaitGroup
	latiendo.Add(1)
	go func() {
		defer latiendo.Done()
		t := time.NewTicker(LatidoIntervalo)
		defer t.Stop()
		for {
			select {
			case <-pararLatido:
				return
			case <-t.C:
				latMu.Lock()
				l := latActual
				latMu.Unlock()
				if l.ID != "" {
					EscribirLatido(o.Root, l)
				}
			}
		}
	}()
	defer func() {
		close(pararLatido)
		latiendo.Wait() // que no quede una escritura en vuelo tras el borrado
	}()

	// una suite sellada a mano (loom task seal) manda sobre el
	// examinador automático: el usuario ya pagó esas pruebas y volver a
	// generarlas las pisaría.
	var fallaExamen string
	if sealed.Exists(o.Root, o.Task.ID) {
		suiteManualEnCuarto(o.Root, o.Task.ID, o.Room.Path)
	} else if o.Examinador != nil {
		avisar(1, FaseExamen)
		// degrada en abierto: un examinador que falla no frena al
		// implementador. Pero deja de degradar en SILENCIO: el motivo va
		// a examinador.log y al diagnóstico de la tarea sin suite, que
		// era imposible de depurar sin volver a correrla.
		// hayExamen: la prueba visible es del examinador. Puede venir
		// con error (la oculta no se pudo sellar) y sigue valiendo.
		hayExamen, err := o.Examinador.Run(ctx, o.Room.Path)
		if err != nil {
			fallaExamen = err.Error()
			guardarExamen(o.Root, o.Task.ID, err)
		}
		if o.Task.ExamenEsqueleto && !hayExamen {
			// Una suite que no se pudo generar no debe dejar a la tarea
			// imposible: el implementador recupera el archivo de prueba.
			o.PatronesPrueba = []string{}
			o.Task.TocarSolo = conPruebasPropias(o.Task)
			if o.Task.ExamenVisible != "" {
				o.Task.TocarSolo = append(o.Task.TocarSolo, o.Task.ExamenVisible)
			}
		}
	}

	var prevErr string
	if o.Task.ExamenEsqueleto && len(o.PatronesPrueba) == 0 {
		prevErr = "El examen ciego no se pudo preparar. Escribe la prueba en " + o.Task.ExamenVisible + " desde Casos: y hazla pasar. " + fallaExamen
	}
	var noCorrio bool
	for intento := 1; intento <= limite; intento++ {
		inicio := time.Now().UTC()
		avisar(intento, FaseAgente)

		// el punto contra el que se mide ESTE intento. No sirve HEAD al
		// final: si el agente commitea por su cuenta dentro del cuarto,
		// HEAD ya se movió y el intento mediría cero. Si no se puede
		// resolver, "HEAD" al menos compara contra el árbol de trabajo.
		antes := "HEAD"
		if h, err := resolveCommit(o.Room.Path, "HEAD"); err == nil {
			antes = h
		}

		req := Request{
			RoomPath:     o.Room.Path,
			Prompt:       promptPara(o.Task, o.Interfaces, o.Constitucion, o.Skills, o.SkillsContenido, prevErr, len(o.PatronesPrueba) == 0) + alcancePara(o.Room.Path),
			AllowedGlobs: o.Task.TocarSolo,
			Model:        o.Model,
			Timeout:      o.AgentTimeout,
			Env:          o.Env,
		}
		res, agentErr := o.Agent.Run(ctx, req)
		numero := len(previos) + intento
		logRel := guardarLog(o.Root, o.Task.ID, numero, req.Prompt, res, agentErr)

		// la recursión agotó sus caminos: no hay que correr listo_cuando
		// ni gastar los intentos restantes, se detiene con su motivo
		var detener *ErrDetener
		errors.As(agentErr, &detener)

		revertidos, parche, err := revertirConParche(o.Room.Path, antes, o.Task.TocarSolo, o.PatronesPrueba)
		if err != nil {
			return Outcome{}, err
		}
		parcheRel := ""
		if parche != "" {
			parcheRel = filepath.Join(".loom", "runs", o.Task.ID, "fuera-de-alcance.patch")
			if os.WriteFile(filepath.Join(o.Root, parcheRel), []byte(parche), 0o644) != nil {
				parcheRel = ""
			}
		}
		// indexar lo que queda (todo dentro de alcance) para que los
		// archivos nuevos del intento cuenten en el diff
		if _, err := gitRun(o.Room.Path, "add", "-A"); err != nil {
			return Outcome{}, err
		}
		archivos, err := filesSince(o.Room.Path, antes)
		if err != nil {
			return Outcome{}, err
		}
		mas, menos, err := numstatSince(o.Room.Path, antes)
		if err != nil {
			return Outcome{}, err
		}
		simbolos, err := simbolosExportados(o.Room.Path, base)
		if err != nil {
			return Outcome{}, err
		}

		// el punto de restauración se guarda antes de verificar, para
		// que el trabajo verde quede commiteado y ship pueda aplanarlo
		if err := commitWip(o.Room.Path, o.Task.ID, numero); err != nil {
			return Outcome{}, err
		}

		avisar(intento, FaseVerificando)
		var salida string
		var code *int
		if detener == nil {
			salida, code = runPrueba(ctx, o.Room.Path, o.Task.ListoCuando, o.PruebaTimeout, o.Env)
			// verde sin la prueba de la tarea: el runner ignoró el archivo
			// que listo_cuando nombra y todavía no existe, y corrió solo las
			// pruebas viejas. El comportamiento nuevo no lo juzgó nadie.
			if code != nil && *code == 0 {
				if f := task.PruebaSinEscribir(o.Room.Path, o.Task.ListoCuando); f != "" {
					uno := 1
					code = &uno
					salida = "listo_cuando pasó sin la prueba de esta tarea: " + f + " no existe. Escríbela con los casos del contrato; las otras pruebas del comando ya pasaban antes de tu cambio."
				} else if len(o.PatronesPrueba) == 0 && pruebaComplaciente(ctx, o, base) {
					uno := 1
					code = &uno
					salida = "La prueba que escribiste pasa igual SIN tu implementación: se devolvió el código a como estaba antes de la tarea, se dejó tu prueba y listo_cuando siguió verde. Así no prueba nada de lo que pide el contrato. Reescríbela para que ejercite el código nuevo y falle sin él (llama a lo que implementaste y compara con los Casos: del contrato)."
				}
			}
		}
		pasaron, fallaron := ParseTestCounts(salida)
		fin := time.Now().UTC()

		// los campos de lista son arrays en el contrato, no null: un
		// intento sin cambios ni reversiones es [], no nada
		if archivos == nil {
			archivos = []string{}
		}
		if revertidos == nil {
			revertidos = []string{}
		}

		acumulado.Entrada += res.Tokens.Entrada
		acumulado.Salida += res.Tokens.Salida
		acumulado.CacheLeida += res.Tokens.CacheLeida
		acumulado.CacheEscrita += res.Tokens.CacheEscrita
		acumulado.Turnos += res.Tokens.Turnos
		acumulado.CostoUSD += res.Tokens.CostoUSD
		if acumulado.PrimerTurno == 0 {
			acumulado.PrimerTurno, acumulado.PrimerTurnoEscrita = res.Tokens.PrimerTurno, res.Tokens.PrimerTurnoEscrita
		}

		codigoAgente := res.ExitCode
		a := Attempt{
			Intento:                  numero,
			Base:                     arbolBase,
			Inicio:                   inicio,
			Fin:                      fin,
			SalidaCodigo:             code,
			TestsPasaron:             pasaron,
			TestsFallaron:            fallaron,
			ArchivosTocados:          archivos,
			SimbolosExportados:       simbolos,
			LineasMas:                mas,
			LineasMenos:              menos,
			RevertidosFueraDeAlcance: revertidos,
			Tokens:                   res.Tokens,
			Modelo:                   o.Model,
			AgenteSalidaCodigo:       &codigoAgente,
			Log:                      logRel,
		}
		if agentErr != nil {
			a.ErrorAgente = diagnostico(res, agentErr)
		}

		// el gasto de este intento ya ocurrió: se registra (verde o rojo)
		// y, si deja un tope al límite o lo pasa, el intento siguiente no
		// se gasta. Un intento verde siempre gana sobre el corte: el
		// trabajo está hecho, no se descarta por haber pasado el tope.
		n := res.Tokens.Gasto()
		if o.Ventanas != nil && o.Proveedor != "" {
			if !o.Ventanas.Registrar(o.Proveedor, n) {
				_ = s.Append(a)
				if code != nil && *code == 0 {
					return Outcome{Verde: true, Intentos: intento}, nil
				}
				return Outcome{Verde: false, Intentos: intento, UltimoError: MotivoVentanas, Pregunta: MotivoVentanas}, nil
			}
		}
		if o.Presupuesto != nil && !o.Presupuesto.Gastar(n) {
			_ = s.Append(a)
			if code != nil && *code == 0 {
				return Outcome{Verde: true, Intentos: intento}, nil
			}
			return Outcome{Verde: false, Intentos: intento, UltimoError: MotivoPresupuesto, Pregunta: MotivoPresupuesto}, nil
		}

		// la recursión dijo "no hay más camino": detenerse es el
		// resultado, no un fallo del mecanismo (mismo criterio que
		// agotar intentos), pero con el motivo exacto de la hoja roja
		if detener != nil {
			_ = s.Append(a)
			return Outcome{Verde: false, Intentos: intento, UltimoError: detener.Motivo, Pregunta: detener.Motivo}, nil
		}

		// salir con 0 sin haber ejecutado una sola prueba no es verde:
		// no hay oráculo que juzgue la tarea. Pasa cuando el
		// implementador escribió su propia suite y la reversión de
		// alcance la quitó, o cuando el examinador ciego degradó
		// y nunca hubo suite. Reintentar no lo arregla —el agente no
		// puede tocar las pruebas—, así que se detiene con el motivo.
		if code != nil && *code == 0 && SinPruebas(salida) {
			motivo := fmt.Sprintf("listo_cuando pasó sin ejecutar ninguna prueba · %s no tiene suite que lo juzgue · sella una con loom task seal %s o apunta listo_cuando a pruebas que existan", o.Task.ID, o.Task.ID)
			if fallaExamen != "" {
				motivo += " · " + fallaExamen
			}
			if err := s.Append(a); err != nil {
				return Outcome{}, err
			}
			return Outcome{Verde: false, Intentos: intento, UltimoError: motivo, Pregunta: motivo}, nil
		}

		// tests verdes no implican contrato cumplido: el revisor juzga
		// el diff contra la tarea y puede vetar. Si pide cambios, el
		// intento queda rojo y su veredicto va al prompt siguiente.
		// Degrada en abierto: un revisor que no responde no frena
		// trabajo que ya está verde.
		visual := o.RevisorVisual != nil && skills.TocaUI(o.Task.TocarSolo) && rechazosVisuales < TopeVisual
		if code != nil && *code == 0 && (o.Revisor != nil || visual) {
			avisar(intento, FaseRevision)
			gastar := func(tk Tokens) {
				acumulado.Entrada += tk.Entrada
				acumulado.Salida += tk.Salida
				acumulado.CacheLeida += tk.CacheLeida
				acumulado.CacheEscrita += tk.CacheEscrita
				m := tk.Gasto()
				if o.Ventanas != nil && o.Proveedor != "" {
					_ = o.Ventanas.Registrar(o.Proveedor, m)
				}
				if o.Presupuesto != nil {
					_ = o.Presupuesto.Gastar(m)
				}
			}
			aprobada, motivo := true, ""
			a.Revision = &Revision{Aprobada: true}
			if o.Revisor != nil {
				var diffClaro string
				if d, err := gitRun(o.Room.Path, "diff", base+"...HEAD"); err == nil {
					diffClaro = d
				}
				var cambios string
				var tk Tokens
				aprobada, cambios, tk = o.Revisor.Revisar(ctx, o.Room.Path, o.Task, diffClaro, intento)
				a.Revision = &Revision{Aprobada: aprobada, Cambios: cambios}
				gastar(tk)
				motivo = "el revisor pide cambios sobre tests verdes:\n" + cambios
			}
			// el código está bien: falta que se vea. Solo lo mira quien ve
			// capturas; las pruebas de UI no saben si un efecto se nota
			if aprobada && visual {
				okV, cambiosV, tk := o.RevisorVisual.Revisar(ctx, o.Room, o.Task, intento)
				gastar(tk)
				if !okV {
					rechazosVisuales++
					aprobada = false
					a.Revision = &Revision{Aprobada: false, Visual: cambiosV}
					motivo = "la revisión visual (capturas de tu pantalla en el celular) pide cambios:\n" + cambiosV
				}
			}
			if err := s.Append(a); err != nil {
				return Outcome{}, err
			}
			if aprobada {
				return Outcome{Verde: true, Intentos: intento}, nil
			}
			prevErr = motivo
			continue
		}

		if err := s.Append(a); err != nil {
			return Outcome{}, err
		}

		if code != nil && *code == 0 {
			return Outcome{Verde: true, Intentos: intento}, nil
		}

		// el agente no llegó al modelo: reintentar no cambia nada, y
		// gastar los intentos restantes solo retrasa el diagnóstico
		if falloDeInfra(res, agentErr, archivos) {
			motivo := fmt.Sprintf("el agente no pudo ejecutarse (%s) · revisa el modelo y la key con loom doctor", a.ErrorAgente)
			if logRel != "" {
				motivo += " · detalle en " + logRel
			}
			return Outcome{Verde: false, Intentos: intento, UltimoError: a.ErrorAgente, Pregunta: motivo}, nil
		}

		// un intento que no dejó nada dentro del alcance deja el código
		// igual que el anterior: repetir da el mismo rojo. No se compara
		// la salida, que cambia por tiempos (vitest) aunque nada cambie.
		if intento > 1 && len(archivos) == 0 {
			motivo := "sin progreso: el intento no dejó cambios dentro del alcance · revisa el contrato"
			if len(revertidos) > 0 {
				motivo = fmt.Sprintf("sin progreso: el agente solo tocó archivos fuera de tocar_solo (%s) y se revirtieron · agrégalos a tocar_solo si la tarea los necesita", strings.Join(revertidos, ", "))
				if parcheRel != "" {
					motivo += fmt.Sprintf(" · si arregla algo que ya estaba roto en la base, su cambio quedó en %s: aplícalo en la base con git apply, haz commit y reintenta", parcheRel)
				}
			}
			return Outcome{Intentos: intento, UltimoError: resumenFallo(o.Task.ListoCuando, code, salida), Pregunta: motivo, NoEscalar: len(revertidos) > 0 || PruebaNoCorrio(o.Task.ListoCuando, code, salida)}, nil
		}
		prevErr = resumenFallo(o.Task.ListoCuando, code, salida)
		noCorrio = PruebaNoCorrio(o.Task.ListoCuando, code, salida)
		if agentErr != nil {
			prevErr = strings.TrimSpace(a.ErrorAgente + " · " + prevErr)
		}
	}

	pregunta := fmt.Sprintf("agotó %d intentos · falla: %s", limite, prevErr)
	return Outcome{
		Verde:       false,
		Intentos:    limite,
		UltimoError: prevErr,
		Pregunta:    pregunta,
		NoEscalar:   noCorrio,
	}, nil
}

// TopeIntentos multiplica limite_intentos para dar el tope total de una
// tarea: su escalón y uno de escalada.
const TopeIntentos = 2

// intentosPagados cuenta los intentos que gastaron tokens desde que se
// editó el contrato y contra la base actual (arbolBase). Los que no
// llegaron al modelo (sin saldo, CLI caído) no cuentan: no costaron nada.
// Un intento sin base anotada es de antes de que se guardara y cuenta.
func intentosPagados(as []Attempt, desde time.Time, arbolBase string) int {
	n := 0
	for _, a := range as {
		if a.Inicio.Before(desde) || a.Tokens.Entrada+a.Tokens.Salida == 0 {
			continue
		}
		if a.Base != "" && arbolBase != "" && a.Base != arbolBase {
			continue
		}
		n++
	}
	return n
}

// contratoEditado es la última modificación del contrato de la tarea;
// cero si no está (las subtareas de la recursión no tienen archivo).
func contratoEditado(root, id string) time.Time {
	info, err := os.Stat(task.Path(config.TasksDir(root), id))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// suiteManualEnCuarto copia al cuarto la suite visible que el usuario
// selló con `loom task seal`. Aterriza en la misma ruta que usaría el
// examinador automático, así que de acá para abajo nadie distingue el
// origen. Degrada igual que el examinador: si algo falla, el implementador
// corre sin suite visible, pero nunca se lo frena.
func suiteManualEnCuarto(root, id, roomPath string) {
	s, err := sealed.Read(root, id)
	if err != nil || s.Visible == "" || s.ArchivoVisible == "" {
		return
	}
	abs := filepath.Join(roomPath, filepath.FromSlash(s.ArchivoVisible))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(abs, []byte(s.Visible), 0o644); err != nil {
		return
	}
	// commitear o revertFueraDeAlcance la borra en el primer
	// intento: git status lista lo que cambió, no lo ya commiteado.
	_, _ = gitRun(roomPath, "add", s.ArchivoVisible)
	_, _ = gitRun(roomPath, "-c", "user.name=loom", "-c", "user.email=loom@local",
		"commit", "-m", "exam: suite visible sellada a mano")
}

// commitWip guarda el punto de restauración interno: un commit `wip:` en
// la rama del cuarto. Sin cambios que guardar, no hace nada. Los wip son
// basura intencional que ship aplana; nunca llegan al PR.
func commitWip(roomPath, id string, intento int) error {
	if _, err := gitRun(roomPath, "diff", "--cached", "--quiet"); err == nil {
		return nil // nada que guardar
	}
	msg := fmt.Sprintf("wip: %s intento %d", id, intento)
	if _, err := gitRun(roomPath, "-c", "user.name=loom", "-c", "user.email=loom@local", "commit", "-m", msg); err != nil {
		return fmt.Errorf("no se pudo guardar el punto de restauración · %s", err)
	}
	return nil
}

// runPrueba ejecuta listo_cuando dentro del cuarto y devuelve la salida
// combinada y su código de salida (nil si ni siquiera arrancó).
func runPrueba(ctx context.Context, dir, cmdStr string, timeout time.Duration, env []string) (string, *int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", task.SinTerminal(cmdStr))
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", task.SinTerminal(cmdStr))
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.CombinedOutput()
	code := 0
	if ctx.Err() == context.DeadlineExceeded {
		code = 124
		return string(out), &code
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
			return string(out), &code
		}
		return string(out) + err.Error(), nil
	}
	return string(out), &code
}

// pruebaComplaciente dice si la prueba que el agente escribió pasa también
// sin su implementación. Es la mutación más barata que hay: se devuelve
// todo el código que no es de prueba a como estaba al empezar la tarea,
// se deja la prueba nueva y se corre listo_cuando. Si sigue verde, la
// prueba no ejercita el cambio. Es el contrapeso sin modelo a que el
// mismo agente escriba código y prueba (37 de 38 tareas verdes
// al primer intento, juzgadas por su propia prueba).
//
// Solo opina cuando listo_cuando nombra un archivo de prueba y la tarea
// cambió código que no es de prueba: un Dockerfile verificado con
// `docker compose config` o una tarea que solo escribe pruebas pasarían
// igual sin el cambio y no son complacientes.
func pruebaComplaciente(ctx context.Context, o Options, inicio string) bool {
	if o.Room.Commit != "" {
		inicio = o.Room.Commit
	}
	if inicio == "" || len(task.ArchivosDePrueba(o.Task.ListoCuando)) == 0 {
		return false
	}
	cambiados, err := filesSince(o.Room.Path, inicio)
	if err != nil {
		return false
	}
	var codigo []string
	for _, f := range cambiados {
		if !task.EsArchivoDePrueba(f) {
			codigo = append(codigo, f)
		}
	}
	if len(codigo) == 0 {
		return false
	}
	// pase lo que pase, el cuarto vuelve al commit del intento
	defer func() {
		_, _ = gitRun(o.Room.Path, "checkout", "HEAD", "--", ".")
	}()
	for _, f := range codigo {
		if _, err := gitRun(o.Room.Path, "cat-file", "-e", inicio+":"+f); err == nil {
			if _, err := gitRun(o.Room.Path, "checkout", inicio, "--", f); err != nil {
				return false
			}
		} else if err := os.Remove(filepath.Join(o.Room.Path, f)); err != nil && !os.IsNotExist(err) {
			return false
		}
	}
	salida, code := runPrueba(ctx, o.Room.Path, o.Task.ListoCuando, o.PruebaTimeout, o.Env)
	return code != nil && *code == 0 && !SinPruebas(salida)
}

// conPruebasPropias suma a tocar_solo los archivos de prueba que corre
// listo_cuando. Sin examinador el prompt le pide al agente escribirlos,
// pero si no están en su alcance la reversión los borra: la tarea era
// imposible por diseño. En una corrida real, T-003 (`npm --prefix frontend test --
// src/image/compress.test.ts`) quemó 15 intentos y la escalera a otro
// modelo así. La ruta puede ser relativa a un --prefix o a un `cd`, por
// eso también entra con cualquier prefijo.
// pruebasFijas devuelve los archivos de prueba de listo_cuando que ya
// existían en el commit con que arrancó el cuarto. Se mira ese commit y
// no el árbol: la prueba que el agente escribió en un intento anterior
// sigue siendo suya al escalar o retomar.
//
// Salvo las que el contrato nombra en tocar_solo con su ruta exacta (un
// glob amplio como "src/**" no cuenta: no es una decisión): ahí quien planeó decidió
// que el cambio las deja obsoletas y que la tarea las actualiza. Antes se
// congelaban igual, y una tarea que cambia un comportamiento ya probado
// no podía cerrar: el código nuevo rompe la prueba vieja y la prueba
// vieja no se podía tocar. El contrapeso es el revisor, que ve el diff de
// la prueba contra lo que el contrato declaró obsoleto.
func pruebasFijas(r room.Room, base string, t task.Task) []string {
	nombradas := task.ArchivosDePrueba(t.ListoCuando)
	if len(nombradas) == 0 {
		return nil
	}
	desde := r.Commit
	if desde == "" {
		desde = base
	}
	if desde == "" {
		desde = "HEAD"
	}
	out, err := gitRun(r.Path, "ls-tree", "-r", "--name-only", desde)
	if err != nil {
		return nil
	}
	var fijas []string
	for _, p := range nombradas {
		p = strings.TrimPrefix(p, "./")
		patrones := []string{p, "**/" + p}
		for _, f := range strings.Split(out, "\n") {
			if f != "" && config.MatchesAny(patrones, f) && !slices.Contains(t.TocarSolo, f) {
				fijas = append(fijas, f)
			}
		}
	}
	return fijas
}

func conPruebasPropias(t task.Task) []string {
	alcance := append([]string(nil), t.TocarSolo...)
	for _, p := range task.ArchivosDePrueba(t.ListoCuando) {
		p = strings.TrimPrefix(p, "./")
		if config.MatchesAny(alcance, p) {
			continue
		}
		alcance = append(alcance, p)
		if !strings.HasPrefix(p, "/") {
			alcance = append(alcance, "**/"+p)
		}
	}
	return alcance
}

// promptPara arma el prompt de un intento: el contrato y, si lo hay, la
// salida del intento anterior. El agente nunca decide si terminó.
// pruebasPropias marca que el stack no tiene examinador ciego: la suite
// que listo_cuando ejecuta la escribe el propio agente, y decírselo
// evita que la deje sin crear esperando a un examinador que no existe.
//
// Primero va lo común a todas las tareas del plan —la base de loom,
// constitución, reglas y arquitectura—, idéntico byte a byte, y al final
// lo de la tarea (incluidas las skills que declara). Así el
// inicio del prompt se repite entre agentes e intentos y el caché de
// prompts del proveedor lo reutiliza. Nada variable (ids, rutas del
// cuarto, fechas) puede entrar en la parte común.
func promptPara(t task.Task, interfaces []string, constitucion string, etiquetas []string, skillsContenido string, prevErr string, pruebasPropias bool) string {
	notas := plan.SepararNotas(t.Notas)
	arq := plan.RecortarArquitectura(notas.Arquitectura, t)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", skills.Base)
	if constitucion != "" {
		fmt.Fprintf(&b, "Constitución del proyecto (convenciones que todos los agentes deben seguir):\n%s\n\n", constitucion)
	}
	if notas.Reglas != "" {
		fmt.Fprintf(&b, "%s\n\n", notas.Reglas)
	}
	if arq.Comun != "" {
		fmt.Fprintf(&b, "%s%s\n\n", plan.MarcaArquitectura, arq.Comun)
	}

	if len(etiquetas) > 0 {
		fmt.Fprintf(&b, "Habilidades de este rol: %s\n\n", strings.Join(etiquetas, ", "))
	}
	if skillsContenido != "" {
		fmt.Fprintf(&b, "Skills de esta tarea — sigue sus instrucciones:\n%s\n\n", skillsContenido)
	}
	if skills.TocaUI(t.TocarSolo) {
		fmt.Fprintf(&b, "%s\n\n", skills.UI)
	}
	fmt.Fprintf(&b, "Tarea %s: %s\n", t.ID, t.Titulo)
	if t.Porque != "" {
		fmt.Fprintf(&b, "Por qué: %s\n", t.Porque)
	}
	fmt.Fprintf(&b, "Listo cuando: %s\n", t.ListoCuando)
	if pruebasPropias {
		fmt.Fprintf(&b, "Este proyecto no tiene examinador ciego: la suite de pruebas que verifica tu trabajo la escribes TÚ. Si el listo_cuando apunta a un archivo de prueba (p. ej. src/core/types.test.ts), créalo además del código.\n")
	}
	if len(t.TocarSolo) > 0 {
		fmt.Fprintf(&b, "Solo puedes tocar: %s\n", strings.Join(t.TocarSolo, ", "))
	}
	// la costura: lo que esta tarea debe exponer y lo que otras ya le
	// garantizan. Son contrato, no sugerencia: la esclusa de salida
	// verifica que `expone` aparezca en el diff.
	if len(t.Expone) > 0 {
		fmt.Fprintf(&b, "Debes exponer estas firmas exactas (otras tareas ya las consumen): %s\n", strings.Join(t.Expone, "; "))
	}
	if len(interfaces) > 0 {
		fmt.Fprintf(&b, "Otras tareas exponen esto; úsalo tal cual, no lo redefinas: %s\n", strings.Join(interfaces, "; "))
	}
	// El presupuesto se verifica al entregar, pero el agente nunca se
	// enteraba de que existía: lo estima el planificador antes de que
	// haya código y se hace cumplir cuando el trabajo ya está verde.
	// Decirlo aquí es lo único que le da la chance de cumplirlo.
	if t.LimiteLineas > 0 {
		fmt.Fprintf(&b, "Presupuesto: unas %d líneas añadidas. Ve al grano y no infles el cambio. Si la tarea de verdad no cabe, dilo al terminar en vez de recortar la solución.\n", t.LimiteLineas)
	}
	if t.Riesgos != "" {
		fmt.Fprintf(&b, "Riesgos: %s\n", t.Riesgos)
	}
	// árbol y firmas recortados a lo que la tarea toca, usa o expone
	if arq.Arbol != "" {
		fmt.Fprintf(&b, "\n%s\n", arq.Arbol)
	}
	if arq.Firmas != "" {
		fmt.Fprintf(&b, "\n%s\n", arq.Firmas)
	}
	if notas.Tarea != "" {
		etiqueta := "Notas:"
		if notas.Arquitectura != "" {
			etiqueta = strings.TrimSuffix(plan.MarcaImplementacion, "\n")
		}
		fmt.Fprintf(&b, "\n%s\n%s\n", etiqueta, notas.Tarea)
	}
	if prevErr != "" {
		fmt.Fprintf(&b, "\nEl intento anterior falló:\n%s\n", prevErr)
	}
	return b.String()
}

// resumenFallo describe por qué falló listo_cuando, para el agente y para
// el usuario.
//
// Un comando que no imprime nada es normal y frecuente: el planificador
// escribe cosas como `go run . --help | grep -q -- --mac`, y `grep -q`
// calla por diseño. Antes eso producía literalmente "sin salida", que no
// le dice nada a nadie: el agente recibía ese texto como único dato del
// intento anterior y se quedaba sin saber qué arreglar. Diciendo el
// comando y su código de salida, al menos hay de dónde agarrarse.
func resumenFallo(listoCuando string, code *int, salida string) string {
	if cuerpo := ultimasLineas(sinMarcos(salida), 8, 800); cuerpo != "" {
		return cuerpo
	}
	estado := "falló"
	if code != nil {
		estado = fmt.Sprintf("salió con código %d", *code)
	}
	msg := fmt.Sprintf("listo_cuando %s sin imprimir nada · comando: %s", estado, strings.TrimSpace(listoCuando))
	if silencioso(listoCuando) {
		msg += " · el comando silencia su salida, así que no hay pista del fallo: córrelo a mano sin -q para ver qué falta"
	}
	return msg
}

// silencioso reporta si el comando esconde su propia salida.
func silencioso(cmd string) bool {
	for _, patron := range []string{"grep -q", "-q --", "--quiet", "> /dev/null", ">/dev/null", "&>/dev/null"} {
		if strings.Contains(cmd, patron) {
			return true
		}
	}
	return false
}

// marcoRE reconoce los marcos de un stack trace: en jest o Java llenan la
// cola de la salida y empujan fuera el "Expected/Received" que el agente
// necesita para arreglar algo.
var marcoRE = regexp.MustCompile(`^\s*(at |File "|goroutine \d|\S+\.go:\d+ \+0x|\.\.\. \d+ more)`)

// sinMarcos quita de s las líneas de stack trace.
func sinMarcos(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if !marcoRE.MatchString(l) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// ultimasLineas devuelve las últimas n líneas no vacías de s, acotadas a
// max caracteres. Cadena vacía si no hay nada que mostrar.
func ultimasLineas(s string, n, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var utiles []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			utiles = append(utiles, strings.TrimRight(l, " \t"))
		}
	}
	if len(utiles) == 0 {
		return ""
	}
	if len(utiles) > n {
		utiles = utiles[len(utiles)-n:]
	}
	out := strings.Join(utiles, "\n")
	if len(out) > max {
		out = "…" + out[len(out)-max:]
	}
	return out
}

// valorSalida distingue un proceso sin código de cualquier salida numérica.
func valorSalida(code *int) string {
	if code == nil {
		return "sin código"
	}
	return fmt.Sprint(*code)
}
