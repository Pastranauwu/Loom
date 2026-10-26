package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/gate"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/overlap"
	"github.com/Pastranauwu/loom/internal/recurse"
	"github.com/Pastranauwu/loom/internal/revisor"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/standup"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

// runResult es una tarea de la corrida, con su desenlace.
type runResult struct {
	ID       string `json:"id"`
	Titulo   string `json:"titulo"`
	Estado   string `json:"estado"` // lista | detenida | rechazada
	Intentos int    `json:"intentos,omitempty"`
	Motivo   string `json:"motivo,omitempty"`
	// Tokens es el gasto total de la tarea (suma de todos sus intentos),
	// para que la corrida deje ver cuánto cuesta cada pieza.
	Tokens int `json:"tokens,omitempty"`
}

func newRunCmd() *cobra.Command {
	var agentes int
	var ejecutor string
	var modelo string
	var reintentar bool
	var fondo bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "ejecuta las tareas pendientes en paralelo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCmd(agentes, ejecutor, modelo, reintentar, fondo)
		},
	}
	cmd.Flags().IntVar(&agentes, "agents", 0, "tareas en paralelo (0 = automático, hasta 16 y 3 con modelo de pago)")
	cmd.Flags().StringVar(&ejecutor, "ejecutor", "", "opencode o claude (por defecto, el primero disponible)")
	cmd.Flags().StringVar(&modelo, "modelo", "", "modelo del ejecutor (por defecto, el suyo)")
	cmd.Flags().BoolVar(&reintentar, "retry", false, "vuelve a correr también las tareas detenidas, reusando su cuarto")
	cmd.Flags().BoolVar(&fondo, "background", false, "desprende la corrida de la terminal y devuelve el control; sigue corriendo si la cierras")
	return cmd
}

func runCmd(agentes int, ejecutor, modelo string, reintentar, fondo bool) error {
	if agentes < 0 {
		return errors.New("--agents inválido · mínimo 1")
	}
	root, cfg, err := entornoListo(false)
	if err != nil {
		return err
	}
	// desprenderse DESPUÉS de preparar el entorno: la preparación es lo
	// único que puede preguntar, y preguntar sirve mientras el humano
	// sigue frente a la terminal. El hijo hereda el config.yml ya
	// completo y no necesita contestar nada.
	if fondo {
		return lanzarEnFondo(root)
	}
	defer marcarCorrida(root)()
	constitucion, err := constitucionDe(root)
	if err != nil {
		return err
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return err
	}
	for _, t := range tareas {
		soltarSinRastro(context.Background(), root, t.ID)
	}
	// una entrega cortada deja las ramas aplanadas y las dependientes
	// ya no se pueden juntar ("CONFLICTO (agregar/agregar)")
	if hechas, dudosas := ship.RestaurarPuntas(root); len(hechas)+len(dudosas) > 0 {
		out.Line("· %s", ship.AvisoPuntas(hechas, dudosas))
	}

	var pendientes, existentes []task.Task
	var detenidas, interrumpidas, huerfanas, enOtraCorrida []string
	for _, t := range tareas {
		s, err := state.Get(root, t.ID)
		if err != nil {
			return err
		}
		switch s.Estado {
		case state.Pendiente:
			pendientes = append(pendientes, t)
		case state.EnCurso:
			// `en_curso` con la corrida muerta encima (SIGKILL: la laptop
			// suspende, se cae el ssh) quedaba atascada para siempre —
			// `run` la mandaba a `existentes`, que solo sirve para el
			// cruce, y ni --retry la miraba. Se reconoce por el
			// latido rancio y se revive por el mismo camino que una
			// detenida. No es automático a propósito: si otra corrida la
			// está trabajando de verdad su latido está fresco, y revivirla
			// pondría dos agentes en el mismo cuarto.
			lat, muerta := loop.Interrumpida(root, t.ID)
			// sin otra corrida viva en este repo nadie la está
			// trabajando, diga lo que diga el latido: un Ctrl-C deja el
			// estado en en_curso y el latido fresco por 90 s, y en ese
			// rato `up` respondía "sin tareas pendientes · empieza con
			// task add" con la tarea a medias. Se retoma sola.
			if !otraCorridaViva(root) {
				interrumpidas = append(interrumpidas, t.ID)
				pendientes = append(pendientes, t)
				continue
			}
			if muerta {
				if reintentar {
					interrumpidas = append(interrumpidas, t.ID)
					pendientes = append(pendientes, t)
				} else {
					huerfanas = append(huerfanas, fmt.Sprintf("%s (sin señal hace %s)", t.ID, lat.Silencio().Round(time.Second)))
				}
				continue
			}
			enOtraCorrida = append(enOtraCorrida, t.ID)
			existentes = append(existentes, t)
		case state.Detenida:
			// una tarea detenida quedaba muerta para siempre: `run` solo
			// miraba las pendientes y no había comando que la reviviera.
			if reintentar {
				detenidas = append(detenidas, t.ID)
				pendientes = append(pendientes, t)
			}
		}
	}
	if len(pendientes) == 0 {
		// las huérfanas van primero: son las que el humano no sabe que
		// tiene, porque el tablero las pintaba corriendo
		if len(huerfanas) > 0 {
			out.Line("sin tareas pendientes · %s quedó de una corrida que murió · retómala con loom run --retry", strings.Join(huerfanas, ", "))
			return nil
		}
		if len(detenidas) == 0 && tieneDetenidas(root, tareas) {
			out.Line("sin tareas pendientes · hay tareas detenidas · revíveles con loom run --retry")
			return nil
		}
		if len(enOtraCorrida) > 0 {
			out.Line("sin tareas pendientes · %s ya la está corriendo otra corrida de este repo · mírala con loom board o párala con loom stop", strings.Join(enOtraCorrida, ", "))
			return nil
		}
		out.Line("sin tareas pendientes · empieza con loom task add \"lo que necesitas\"")
		return nil
	}
	if perdidas := verdesPerdidas(context.Background(), root, cfg.Base, pendientes); len(perdidas) > 0 {
		return fmt.Errorf("%s figuran listas pero su rama ya no existe y su trabajo no está en %s · las tareas que dependen de ellas arrancarían sin ese código · borra .loom y replanea con loom up, o quítalas con loom task rm si ya están entregadas", strings.Join(perdidas, ", "), cfg.Base)
	}
	if len(detenidas) > 0 {
		out.Line("· reintentando %s · se reusa el cuarto y el trabajo parcial de cada una", strings.Join(detenidas, ", "))
	}
	if len(interrumpidas) > 0 {
		out.Line("· retomando %s · su corrida anterior murió a media tarea", strings.Join(interrumpidas, ", "))
	}
	if len(huerfanas) > 0 {
		out.Line("· %s quedó de una corrida que murió · retómala con loom run --retry", strings.Join(huerfanas, ", "))
	}

	timeout := gate.DefaultTimeout
	if cfg.TimeoutEsclusa > 0 {
		timeout = time.Duration(cfg.TimeoutEsclusa) * time.Second
	}
	results := make([]runResult, 0, len(pendientes))

	// esclusa de entrada por tarea: la que no pasa, se rechaza antes de
	// gastar un solo token
	var aprobadas []task.Task
	for _, t := range pendientes {
		// la veda de rutas de prueba se decide por tarea y no por
		// proyecto, igual que en el bucle: donde no hay examen ciego que
		// proteger, vedarle las pruebas deja la tarea imposible de
		// terminar. Con la veda del proyecto, una tarea de integración
		// —que no expone nada y solo escribe pruebas— era rechazada sin
		// llegar a correr.
		cfgEsclusa := cfg
		cfgEsclusa.PatronesPrueba = patronesPruebaTarea(cfg, root, t)
		res := gate.Run(context.Background(), root, cfgEsclusa, t, existentes, timeout)
		if res.Aprobada {
			aprobadas = append(aprobadas, t)
			continue
		}
		results = append(results, runResult{ID: t.ID, Titulo: t.Titulo, Estado: "rechazada", Motivo: motivoRechazo(res)})
	}

	// la costura entre tareas: nadie puede consumir una firma que nadie promete.
	// gate.Run no puede verlo: solo recibe las tareas en_curso, y quien
	// expone suele estar pendiente en la misma corrida.
	aprobadas, results = rechazarUsaHuerfano(aprobadas, tareas, results)

	if len(aprobadas) == 0 {
		sortRunResults(results)
		_ = emitirResultados(results)
		return errors.New("ninguna tarea superó la esclusa · si el motivo es \"listo_cuando ya pasa\", " +
			"el comando tiene que fallar HOY y pasar cuando la tarea esté hecha: vuelve a planear con loom plan")
	}

	if ejecutor == "" {
		ejecutor = cfg.Cli
	}
	ex, err := elegirEjecutor(ejecutor)
	if err != nil {
		return err
	}

	presupuesto := budget.Nuevo(cfg.PresupuestoTokens)
	ventanasReg := ventanas.Nuevo(ventanas.LedgerPath(), cfg.PresupuestoVentanas)

	if esTUI() {
		if len(results) > 0 {
			sortRunResults(results)
			_ = emitirResultados(results)
		}
		olas, err := correrConTUI(context.Background(), root, cfg, ex, modelo, constitucion, aprobadas, agentes, presupuesto, ventanasReg)
		if err != nil {
			return err
		}
		// el tablero se cierra y se lleva lo que mostraba. Sin esto una
		// corrida detenida entera (una rama base inexistente, por
		// ejemplo) no dejaba ni una línea diciendo por qué.
		sortRunResults(olas)
		return emitirResultados(olas)
	}

	if cfg.PresupuestoTokens > 0 {
		out.Line("presupuesto absoluto %s tokens · quemados %s · quedan %s", budget.FormatearGasto(cfg.PresupuestoTokens), budget.FormatearGasto(budget.GastoEnDisco(root)), budget.FormatearGasto(cfg.PresupuestoTokens-budget.GastoEnDisco(root)))
	}
	for _, p := range config.Clis {
		if l := ventanas.LineaVentanas(ventanasReg, p); l != "" {
			out.Line("presupuesto %s", l)
		}
	}
	results = append(results, ejecutarOlas(context.Background(), root, cfg, ex, modelo, constitucion, aprobadas, agentes, presupuesto, ventanasReg, nil)...)
	sortRunResults(results)
	if cfg.PresupuestoTokens > 0 {
		out.Line("presupuesto absoluto %s · gasto final %s · quedan %s", budget.FormatearGasto(cfg.PresupuestoTokens), budget.FormatearGasto(budget.GastoEnDisco(root)), budget.FormatearGasto(cfg.PresupuestoTokens-budget.GastoEnDisco(root)))
	}
	return emitirResultados(results)
}

// correrConTUI ejecuta la corrida dentro del tablero en vivo y devuelve
// los resultados para imprimirlos al final.
func correrConTUI(ctx context.Context, root string, cfg config.Config, ex executor.Executor, modelo, constitucion string, aprobadas []task.Task, agentes int, presupuesto *budget.Contador, ventanasReg *ventanas.Registro) ([]runResult, error) {
	filas := make([]tui.FilaRun, 0, len(aprobadas))
	for _, t := range aprobadas {
		filas = append(filas, tui.FilaRun{ID: t.ID, Titulo: t.Titulo, Limite: t.LimiteIntentos})
	}
	ids := make([]string, 0, len(aprobadas))
	for _, t := range aprobadas {
		ids = append(ids, t.ID)
	}

	var results []runResult
	err := tui.CorrerRun(filas, func() map[string]tui.FaseRun { return fasesVivas(root, ids) },
		func(emit func(tui.EventoRun)) {
			results = ejecutarOlas(ctx, root, cfg, ex, modelo, constitucion, aprobadas, agentes, presupuesto, ventanasReg, emit)
		})
	return results, err
}

// ejecutarOlas corre las tareas por oleadas según depende_de:
// una ola solo arranca cuando sus dependencias ya están verdes. El
// trabajo verde de cada ola se integra en una rama temporal y la
// siguiente ola arranca desde ahí, así la cadena comparte estado.
func ejecutarOlas(ctx context.Context, root string, cfg config.Config, ex executor.Executor, modelo, constitucion string, aprobadas []task.Task, agentes int, presupuesto *budget.Contador, ventanasReg *ventanas.Registro, emit func(tui.EventoRun)) []runResult {
	var results []runResult
	procesadas := map[string]bool{}
	integrada := map[string]bool{}

	tieneDeps := false
	for _, t := range aprobadas {
		if len(t.DependeDe) > 0 {
			tieneDeps = true
			break
		}
	}

	base := cfg.Base
	if tieneDeps {
		if err := room.ResetIntegration(ctx, root, cfg.Base); err != nil {
			for _, t := range aprobadas {
				r := runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Motivo: err.Error()}
				results = append(results, r)
				if emit != nil {
					// sin esto el tablero se queda en "pendiente" hasta
					// cerrarse, como si no hubiera pasado nada
					emit(tui.EventoRun{ID: r.ID, Estado: r.Estado, Motivo: r.Motivo})
				}
			}
			return results
		}
		base = room.IntegrationBranch
		results = append(results, sembrarVerdesPrevias(ctx, root, aprobadas, integrada)...)
	}

	// las olas siguientes a un 402 chocarían con lo mismo: quedan
	// pendientes, no bloqueadas
	pendientesSinSaldo := func() {
		for _, t := range aprobadas {
			if !procesadas[t.ID] {
				results = append(results, runResult{ID: t.ID, Titulo: t.Titulo, Estado: estadoSinSaldo, Motivo: "no se lanzó · el proveedor no tiene saldo"})
				procesadas[t.ID] = true
			}
		}
	}

	for {
		var ola []task.Task
		for _, t := range aprobadas {
			if procesadas[t.ID] {
				continue
			}
			if depsVerdes(t.DependeDe, integrada) {
				ola = append(ola, t)
			}
		}

		if len(ola) == 0 {
			for _, t := range aprobadas {
				if procesadas[t.ID] {
					continue
				}
				results = append(results, runResult{
					ID: t.ID, Titulo: t.Titulo, Estado: "rechazada",
					Motivo: "bloqueada · depende de " + strings.Join(depsFaltantes(t.DependeDe, integrada), ", ") + " que no salió verde",
				})
				procesadas[t.ID] = true
			}
			break
		}

		asignadas, rechazadas := asignar(ola)
		for _, r := range rechazadas {
			results = append(results, r)
			procesadas[r.ID] = true
		}

		var verdes []runResult
		sinSaldo := false
		if len(asignadas) > 0 {
			// --agents explícito manda sobre el tope de pagados
			workers, pagados := agentes, agentes
			if workers < 1 {
				workers, pagados = autoAgentes(len(asignadas)), topePagados
				if cfg.AgentesPagados > 0 {
					pagados = cfg.AgentesPagados
				}
			}
			r := correr(ctx, root, cfg, ex, modelo, constitucion, base, asignadas, workers, pagados, presupuesto, ventanasReg, emit)
			results = append(results, r...)
			for _, rr := range r {
				procesadas[rr.ID] = true
				sinSaldo = sinSaldo || rr.Estado == estadoSinSaldo
				if rr.Estado == "lista" {
					verdes = append(verdes, rr)
				}
			}
		}

		if !tieneDeps {
			if sinSaldo {
				pendientesSinSaldo()
				break
			}
			continue
		}
		sort.Slice(verdes, func(i, j int) bool { return verdes[i].ID < verdes[j].ID })
		for _, v := range verdes {
			if salida, err := room.Integrar(ctx, root, v.ID); err != nil {
				results = append(results, runResult{
					ID: v.ID, Titulo: v.Titulo, Estado: "detenida",
					Motivo: "no se pudo integrar con la oleada anterior · " + salida,
				})
				continue
			}
			integrada[v.ID] = true
		}
		if sinSaldo {
			pendientesSinSaldo()
			break
		}
	}
	return results
}

// rechazarUsaHuerfano saca las tareas que consumen una firma que ningún
// contrato del proyecto expone. Es un plan incoherente: el
// agente iba a inventar esa interfaz, y el desajuste recién aparecería
// al juntar las dos ramas.
func rechazarUsaHuerfano(aprobadas, todas []task.Task, results []runResult) ([]task.Task, []runResult) {
	expuestas := map[string]bool{}
	for _, t := range todas {
		for _, f := range t.Expone {
			if n, ok := task.FirmaVerificable(f); ok {
				expuestas[n] = true
			}
		}
	}

	var ok []task.Task
	for _, t := range aprobadas {
		var huerfanas []string
		for _, f := range t.Usa {
			// una descripción en prosa no se puede casar con ningún
			// expone: rechazar por eso mataría un plan por la redacción
			n, verificable := task.FirmaVerificable(f)
			if verificable && !expuestas[n] {
				huerfanas = append(huerfanas, f)
			}
		}
		if len(huerfanas) == 0 {
			ok = append(ok, t)
			continue
		}
		results = append(results, runResult{
			ID: t.ID, Titulo: t.Titulo, Estado: "rechazada",
			Motivo: "usa una firma que ninguna tarea expone: " + strings.Join(huerfanas, "; ") + " · agrégala al expone de quien la produce",
		})
	}
	return ok, results
}

// sembrarVerdesPrevias marca como verdes las dependencias que ya
// quedaron `lista` en una corrida anterior, e integra su rama para que
// la oleada nueva vea ese código.
//
// Sin esto, `integrada` arrancaba vacío en cada corrida: una tarea que
// dependía de trabajo ya hecho se rechazaba con "bloqueada · depende de
// T-00X que no salió verde" para siempre, y no había forma de avanzar
// salvo rehacer todo de una sola corrida. Una dependencia verde cuya
// rama ya no existe se da por satisfecha: `ship` la entregó y liberó su
// cuarto, así que su código ya está en la rama base.
func sembrarVerdesPrevias(ctx context.Context, root string, aprobadas []task.Task, integrada map[string]bool) []runResult {
	enOla := map[string]bool{}
	for _, t := range aprobadas {
		enOla[t.ID] = true
	}

	var previas []string
	vistas := map[string]bool{}
	for _, t := range aprobadas {
		for _, d := range t.DependeDe {
			if enOla[d] || vistas[d] {
				continue
			}
			vistas[d] = true
			previas = append(previas, d)
		}
	}
	sort.Strings(previas)

	var results []runResult
	for _, id := range previas {
		s, err := state.Get(root, id)
		if err != nil || s.Estado != state.Lista {
			continue
		}
		if !room.RamaExiste(ctx, root, id) {
			// verde y ya entregada: su código vive en la rama base
			integrada[id] = true
			continue
		}
		if salida, err := room.Integrar(ctx, root, id); err != nil {
			results = append(results, runResult{
				ID: id, Estado: "detenida",
				Motivo: "no se pudo reusar el trabajo verde de una corrida anterior · " + salida,
			})
			continue
		}
		integrada[id] = true
	}
	return results
}

// verdesPerdidas devuelve las dependencias de las tareas por correr que
// están `lista` sin rama y sin su trabajo en la base. sembrarVerdesPrevias
// lee "lista y sin rama" como "ya entregada"; con las ramas borradas a
// mano eso mandó a los agentes a escribir encima de un esqueleto que no
// estaba: verdes contra módulos inexistentes y tareas sin salida.
func verdesPerdidas(ctx context.Context, root, base string, porCorrer []task.Task) []string {
	var entregadas map[string]bool
	vistas := map[string]bool{}
	var perdidas []string
	for _, t := range porCorrer {
		for _, d := range t.DependeDe {
			if vistas[d] {
				continue
			}
			vistas[d] = true
			if s, err := state.Get(root, d); err != nil || s.Estado != state.Lista || room.RamaExiste(ctx, root, d) {
				continue
			}
			if entregadas == nil {
				entregadas = ship.Entregadas(ctx, root, base)
			}
			if !entregadas[d] {
				perdidas = append(perdidas, d)
			}
		}
	}
	sort.Strings(perdidas)
	return perdidas
}

// depsVerdes reporta si todas las dependencias de una tarea ya salieron
// verdes. Sin dependencias, siempre verde.
func depsVerdes(deps []string, verde map[string]bool) bool {
	for _, d := range deps {
		if !verde[d] {
			return false
		}
	}
	return true
}

// depsFaltantes lista las dependencias que no salieron verdes.
func depsFaltantes(deps []string, verde map[string]bool) []string {
	var faltantes []string
	for _, d := range deps {
		if !verde[d] {
			faltantes = append(faltantes, d)
		}
	}
	return faltantes
}

// asignar reparte las tareas aprobadas para correr juntas: aplica el
// chequeo de cruce al conjunto (tocar_solo vacío solo vale en
// solitario) y descarta las que
// se cruzan, en orden de ID.
func asignar(aprobadas []task.Task) ([]task.Task, []runResult) {
	sort.Slice(aprobadas, func(i, j int) bool { return aprobadas[i].ID < aprobadas[j].ID })
	if len(aprobadas) <= 1 {
		return aprobadas, nil
	}

	var rechazos []runResult
	var conAlcance []task.Task
	for _, t := range aprobadas {
		if len(t.TocarSolo) == 0 {
			rechazos = append(rechazos, runResult{
				ID: t.ID, Titulo: t.Titulo, Estado: "rechazada",
				Motivo: "tocar_solo obligatorio con más de una tarea activa · declara tus rutas",
			})
			continue
		}
		conAlcance = append(conAlcance, t)
	}

	var aceptadas []task.Task
	for _, t := range conAlcance {
		if c := gate.Cruce(t, aceptadas); !c.OK {
			rechazos = append(rechazos, runResult{ID: t.ID, Titulo: t.Titulo, Estado: "rechazada", Motivo: c.Motivo})
			continue
		}
		aceptadas = append(aceptadas, t)
	}
	return aceptadas, rechazos
}

// modeloParaTarea resuelve el modelo de una tarea: el flag
// --modelo gana; si no, el peso de la tarea (o el de la estrategia
// global) busca en `modelos`; y si no hay, cae al modelo del ejecutor.
func modeloParaTarea(cfg config.Config, flagModelo string, t task.Task) string {
	if flagModelo != "" {
		return flagModelo
	}
	peso := t.Peso
	if peso == "" {
		peso = cfg.PesoPorDefecto()
	}
	if m := cfg.ModeloPeso(peso); m != "" {
		return m
	}
	return config.ModeloRol(cfg, "ejecutor")
}

// elegirEjecutor devuelve el ejecutor pedido o el primero instalado.
func elegirEjecutor(nombre string) (executor.Executor, error) {
	if nombre != "" {
		for _, e := range executor.Todos {
			if e.Name() != nombre {
				continue
			}
			if err := e.Available(); err != nil {
				return nil, err
			}
			return e, nil
		}
		return nil, fmt.Errorf("ejecutor desconocido: %s · usa %s", nombre, strings.Join(executor.Nombres(), ", "))
	}
	for _, e := range executor.Todos {
		if e.Available() == nil {
			return e, nil
		}
	}
	return nil, errors.New("ningún ejecutor disponible · instala " + strings.Join(executor.Nombres(), ", "))
}

// agenteExecutor adapta internal/executor a la interfaz loop.Agent. Es el
// único punto donde el comando toca el ejecutor: el bucle no lo importa.
type agenteExecutor struct {
	ex executor.Executor
}

func (a agenteExecutor) Name() string { return a.ex.Name() }

func (a agenteExecutor) Run(ctx context.Context, req loop.Request) (loop.Result, error) {
	rol := executor.RolImplementador
	if req.Texto {
		rol = executor.RolTexto
	}
	res, err := a.ex.Run(ctx, executor.Request{
		Rol:          rol,
		RoomPath:     req.RoomPath,
		Prompt:       req.Prompt,
		AllowedGlobs: req.AllowedGlobs,
		Model:        req.Model,
		Timeout:      req.Timeout,
		Env:          req.Env,
		Sesion:       req.Sesion,
	})
	return loop.Result{
		Sesion:   res.Sesion,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		Text:     res.Text,
		ExitCode: res.ExitCode,
		Tokens:   tokensDe(res.Tokens),
	}, err
}

// tokensDe traduce el gasto del adaptador al del bucle, caché incluida.
func tokensDe(u executor.Usage) loop.Tokens {
	return loop.Tokens{Entrada: u.Input, Salida: u.Output, CacheLeida: u.CacheRead, CacheEscrita: u.CacheWrite,
		Turnos: u.Turns, PrimerTurno: u.FirstTurn, PrimerTurnoEscrita: u.FirstTurnWrite, UltimoTurno: u.LastTurn, CostoUSD: u.CostUSD}
}

// autoAgentes resuelve el paralelismo cuando --agents no se pasa: tantos
// trabajadores como tareas en la oleada, topado a 16. De esos, solo
// topePagados corren a la vez con un modelo que gasta cuota (correr); el
// resto del cupo es para modelos gratis.
func autoAgentes(tareas int) int {
	if tareas > 16 {
		return 16
	}
	if tareas < 1 {
		return 1
	}
	return tareas
}

// topePagados es cuántas tareas con modelo de pago corren a la vez sin
// --agents: todas gastan la misma ventana de 5 h, y en paralelo solo la
// vacían antes (una corrida perdió 3 de 18 intentos por 429).
//
// Se cambia con `agentes_pagados:` en config.yml.
const topePagados = 3

// correr lanza las tareas con `agentes` trabajadores en paralelo, y de
// ellas como mucho `pagados` con un modelo que gasta cuota. onEvent,
// si no es nil, recibe cada transición para el tablero en vivo.
func correr(ctx context.Context, root string, cfg config.Config, ex executor.Executor, modelo, constitucion, base string, asignadas []task.Task, agentes, pagados int, presupuesto *budget.Contador, ventanasReg *ventanas.Registro, onEvent func(tui.EventoRun)) []runResult {
	// solapamiento activo entre tareas de la misma oleada
	alertasOverlap := checkOverlapOla(root, asignadas)
	for _, a := range alertasOverlap {
		out.Line("⚠ SOLAPAMIENTO  %s", a)
	}

	jobs := make(chan task.Task)
	var sinSaldo atomic.Bool
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]runResult, 0, len(asignadas))

	// las gratis van primero: un trabajador esperando cupo pagado no
	// debe dejar en la fila a una que no espera nada
	esPagada := map[string]bool{}
	for _, t := range asignadas {
		_, m, _, _ := resolverAgenteTarea(cfg, ex, modelo, t)
		esPagada[t.ID] = !gratis(m)
	}
	asignadas = append([]task.Task(nil), asignadas...)
	sort.SliceStable(asignadas, func(i, j int) bool { return !esPagada[asignadas[i].ID] && esPagada[asignadas[j].ID] })
	if pagados < 1 {
		pagados = 1
	}
	cupo := make(chan struct{}, pagados)

	for i := 0; i < agentes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				if sinSaldo.Load() {
					mu.Lock()
					results = append(results, runResult{ID: t.ID, Titulo: t.Titulo, Estado: estadoSinSaldo, Motivo: "no se lanzó · el proveedor no tiene saldo"})
					mu.Unlock()
					continue
				}
				if presupuesto.Agotado() {
					mu.Lock()
					results = append(results, runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Motivo: loop.MotivoPresupuesto})
					mu.Unlock()
					continue
				}
				if onEvent != nil {
					onEvent(tui.EventoRun{ID: t.ID, Estado: "trabajando"})
				}
				if esPagada[t.ID] {
					cupo <- struct{}{}
				}
				r := correrUno(ctx, root, cfg, ex, modelo, base, constitucion, presupuesto, ventanasReg, t)
				if esPagada[t.ID] {
					<-cupo
				}
				if r.Estado == estadoSinSaldo {
					sinSaldo.Store(true)
				}
				if onEvent != nil {
					onEvent(tui.EventoRun{ID: r.ID, Estado: r.Estado, Intentos: r.Intentos, Motivo: r.Motivo})
				}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}
	for _, t := range asignadas {
		jobs <- t
	}
	close(jobs)
	wg.Wait()

	// nivel funcional del solapamiento. Va DESPUÉS de la oleada y no antes como los otros
	// dos: lo que mide es "verdes por separado, rompen juntas", y eso
	// exige que las dos estén verdes. Antes de correr, las ramas están
	// vacías y no hay nada que fusionar.
	for _, a := range checkFuncionalOla(ctx, root, cfg, asignadas, results, alertasOverlap) {
		out.Line("⚠ SOLAPAMIENTO  %s", a)
	}
	return results
}

// checkOverlapOla runs overlap checks between all pairs of tasks in a wave.
// Returns human-readable alerts for any detected overlap.
func checkOverlapOla(root string, tareas []task.Task) []string {
	var alertas []string
	for i, a := range tareas {
		for _, b := range tareas[i+1:] {
			asA, _ := loop.ReadAttempts(root, a.ID)
			asB, _ := loop.ReadAttempts(root, b.ID)
			r := overlap.CheckPar(root, a.ID, b.ID, asA, asB)
			if alert := r.Alerta(); alert != "" {
				alertas = append(alertas, alert)
			}
		}
	}
	return alertas
}

// checkFuncionalOla corre el nivel funcional del solapamiento sobre los pares de
// la oleada que terminaron verdes: fusiona sus dos ramas en seco y corre
// las suites de las dos sobre el resultado.
//
// Dos filtros, y los dos importan. Solo pares verdes, porque una suite
// que ya fallaba en su propia rama no dice nada sobre la fusión. Y solo
// pares sospechosos, porque este es el único nivel que ejecuta código:
// un par limpio en texto y en símbolos no se merece dos suites.
//
// El repaso vuelve a llamar a CheckPar en vez de reusar el de antes de la
// oleada: aquel corrió contra ramas vacías. Lo que encuentre de nuevo se
// reporta, salteando lo que ya se dijo al arrancar.
func checkFuncionalOla(ctx context.Context, root string, cfg config.Config, tareas []task.Task, results []runResult, yaDichas []string) []string {
	verdes := make(map[string]bool, len(results))
	for _, r := range results {
		if r.Estado == "lista" {
			verdes[r.ID] = true
		}
	}
	dicha := make(map[string]bool, len(yaDichas))
	for _, a := range yaDichas {
		dicha[a] = true
	}
	timeout := time.Duration(cfg.TimeoutPruebas) * time.Second

	var alertas []string
	for i, a := range tareas {
		if !verdes[a.ID] {
			continue
		}
		for _, b := range tareas[i+1:] {
			if !verdes[b.ID] {
				continue
			}
			asA, _ := loop.ReadAttempts(root, a.ID)
			asB, _ := loop.ReadAttempts(root, b.ID)
			r := overlap.CheckPar(root, a.ID, b.ID, asA, asB)
			if alerta := r.Alerta(); alerta != "" && !dicha[alerta] {
				alertas = append(alertas, alerta)
			}
			if !r.Sospechoso() {
				continue
			}
			rf := overlap.CheckFuncional(ctx, root, r,
				overlap.Suite{ID: a.ID, ListoCuando: a.ListoCuando},
				overlap.Suite{ID: b.ID, ListoCuando: b.ListoCuando},
				timeout)
			if alerta := rf.Alerta(); alerta != "" {
				alertas = append(alertas, alerta)
			}
		}
	}
	return alertas
}

// resolverAgenteTarea determina el ejecutor, modelo, skills (etiquetas) y
// skill_packages (paquetes reales a inyectar) para una tarea concreta
// (zero-config).
func resolverAgenteTarea(cfg config.Config, defaultEx executor.Executor, flagModelo string, t task.Task) (executor.Executor, string, []string, []string) {
	nombreAgente := t.Agente
	if nombreAgente == "" {
		nombreAgente = "ejecutor"
	}

	cli := cfg.Cli
	if defaultEx != nil {
		cli = defaultEx.Name()
	}

	customAg, isCustom := cfg.Agentes[nombreAgente]

	ex := defaultEx
	if isCustom && customAg.Provider != "" {
		if e, err := elegirEjecutor(customAg.Provider); err == nil {
			ex = e
		}
	} else if ex == nil {
		if ag, ok := cfg.ObtenerAgente(nombreAgente); ok && ag.Provider != "" {
			if e, err := elegirEjecutor(ag.Provider); err == nil {
				ex = e
			}
		}
	}

	modelo := flagModelo
	if modelo == "" {
		if isCustom && customAg.Modelo != "" {
			modelo = customAg.Modelo
		} else if p, ok := cfg.Proveedores[nombreAgente]; ok && p.Modelo != "" {
			modelo = p.Modelo
		} else {
			defaults := config.DefaultAgentes(cli)
			if def, ok := defaults[nombreAgente]; ok && def.Modelo != "" {
				modelo = def.Modelo
			} else {
				modelo = modeloParaTarea(cfg, "", t)
			}
		}
	}

	var etiquetas, paquetes []string
	if ag, ok := cfg.ObtenerAgente(nombreAgente); ok {
		etiquetas = ag.Skills
		paquetes = ag.SkillPackages
	}
	// las que el contrato declara mandan sobre las del rol: el
	// planificador sabe si esta tarea en particular dibuja una interfaz,
	// el rol no
	if t.Skills != nil {
		paquetes = t.Skills
	}

	return ejecutorPara(ex, modelo), modelo, etiquetas, paquetes
}

// ejecutorPara elige el CLI que sabe correr el modelo: un id con
// proveedor ("opencode/mimo-v2.6-flash-free") solo lo corre opencode, y
// los de Anthropic sin proveedor ("claude-sonnet-5", "haiku") solo
// claude; los de OpenAI ("gpt-…") solo codex. Así `modelos:` mezcla
// CLIs: livianas gratis en opencode y el resto en claude o codex. Si el CLI que hace falta no está, queda ex.
func ejecutorPara(ex executor.Executor, modelo string) executor.Executor {
	quiere := ""
	switch {
	case strings.Contains(modelo, "/"):
		quiere = "opencode"
	case strings.HasPrefix(modelo, "claude-"), modelo == "opus", modelo == "sonnet", modelo == "haiku":
		quiere = "claude"
	case strings.HasPrefix(modelo, "gpt-"), strings.HasPrefix(modelo, "codex-"):
		quiere = "codex"
	}
	if quiere == "" || (ex != nil && ex.Name() == quiere) {
		return ex
	}
	if e, err := elegirEjecutor(quiere); err == nil {
		return e
	}
	return ex
}

// gratis reconoce los modelos sin costo por su id: opencode los nombra
// "…-free" y OpenRouter "…:free". No gastan cuota, así que no entran al
// tope de agentes pagados.
func gratis(modelo string) bool {
	return strings.HasSuffix(modelo, "-free") || strings.HasSuffix(modelo, ":free")
}

// revisorEnBucle adapta internal/revisor al loop: corre sobre un intento
// verde en tests y devuelve si la tarea puede darse por cumplida. Degrada
// en abierto: un revisor que no responde no frena trabajo que ya está
// verde (el mismo criterio que el examinador, al revés que el revisor de
// entrega, que falla cerrado). Invoca el ejecutor directo para capturar
// el gasto de la revisión, que cuenta igual que el del agente.
type revisorEnBucle struct {
	ex     executor.Executor
	modelo string
	root   string
}

func (r revisorEnBucle) Revisar(ctx context.Context, _ string, tarea task.Task, diff string, _ int) (bool, string, loop.Tokens) {
	prompt := revisor.Prompt([]task.Task{tarea}, diff)
	res, err := r.ex.Run(ctx, executor.Request{
		Rol:      executor.RolTexto,
		RoomPath: r.root,
		Prompt:   prompt,
		Model:    r.modelo,
		Timeout:  5 * time.Minute,
		// juzgar un diff contra su contrato no pide razonar largo: corre
		// en cada intento verde y el pensamiento extendido es lo que pesa
		Effort: "low",
	})
	tk := tokensDe(res.Tokens)
	if err != nil {
		return true, "", tk // degrada en abierto
	}
	v, err := revisor.Parse(res.Text, []task.Task{tarea})
	if err != nil {
		return true, "", tk // degrada en abierto
	}
	for _, t := range v.Tareas {
		if t.ID == tarea.ID && !t.Funciona {
			return false, strings.Join(t.Cambios, "\n"), tk
		}
	}
	return true, "", tk
}

// revisorParaBucle arma el revisor del bucle con el modelo del rol
// `revisor`, cayendo al medio si no hay nada declarado. Si el CLI no
// está disponible, devuelve nil y el bucle corre sin revisión.
func revisorParaBucle(root string, cfg config.Config, ex executor.Executor) loop.Revisor {
	if ex == nil {
		var err error
		ex, err = elegirEjecutor(cfg.Cli)
		if err != nil {
			return nil
		}
	}
	modelo := config.ModeloRol(cfg, "revisor")
	if modelo == "" {
		modelo = cfg.ModeloPeso("media")
	}
	if modelo == "" {
		modelo = cfg.ModeloPeso("liviana")
	}
	return revisorEnBucle{ex: ejecutorPara(ex, modelo), modelo: modelo, root: root}
}

// modeloExaminador resuelve el modelo del examinador ciego: el rol
// `examinador` declarado en config.yml, o el modelo liviano por defecto.
// Escribir pruebas por tarea es trabajo repetitivo, no arquitectura:
// quemar el modelo pesado ahí multiplica el costo de la corrida sin
// mejorar el examen.
func modeloExaminador(cfg config.Config) string {
	if m := config.ModeloRol(cfg, "examinador"); m != "" {
		return m
	}
	return cfg.ModeloPeso("liviana")
}

// correrUno ejecuta una tarea completa: cuarto, esclusa de estado,
// bucle, y deja el estado final (lista o detenida). El cuarto no se
// destruye aquí: ship lo libera al entregar.
//
// Al igual que en la recursión, una tarea plana que deja trabajo y queda
// roja se reintenta una vez con el siguiente modelo más pesado, reusando
// su cuarto y su trabajo parcial — el modelo barato es el default, y
// subir de escalón no cuesta tokens extra más allá del intento repetido.
func correrUno(ctx context.Context, root string, cfg config.Config, ex executor.Executor, modelo, base, constitucion string, presupuesto *budget.Contador, ventanasReg *ventanas.Registro, t task.Task) runResult {
	r, err := room.Ensure(ctx, root, t.ID, base)
	if err != nil {
		return runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Motivo: err.Error()}
	}

	if err := state.Save(root, state.State{ID: t.ID, Estado: state.EnCurso, Rama: r.Rama, Puerto: r.Puerto, Commit: r.Commit}); err != nil {
		_ = room.Destroy(ctx, root, t.ID)
		return runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Motivo: err.Error()}
	}

	exTarea, modeloTarea, skillsTarea, skillPkgsTarea := resolverAgenteTarea(cfg, ex, modelo, t)
	skillsContenido := skills.Content(root, skillPkgsTarea)

	var agentTimeout, pruebaTimeout time.Duration
	if cfg.TimeoutAgente > 0 {
		agentTimeout = time.Duration(cfg.TimeoutAgente) * time.Second
	}
	if cfg.TimeoutPruebas > 0 {
		pruebaTimeout = time.Duration(cfg.TimeoutPruebas) * time.Second
	}

	// el examen ciego es una invocación de agente como cualquier otra y
	// cobra el mismo timeout. Con 3 minutos fijos, la tarea con la
	// frontera más grande —la que más pruebas necesita— era justo la que
	// se quedaba sin suite: el examinador no alcanzaba a responder,
	// degradaba, y la tarea moría con "listo_cuando pasó sin ejecutar
	// ninguna prueba" porque la veda le quita al implementador las suyas.
	examEx := ejecutorPara(exTarea, modeloExaminador(cfg))
	exam := examiner.Runner{Options: examiner.Options{
		Agent:    agenteExecutor{examEx},
		Task:     t,
		Root:     root,
		Model:    modeloExaminador(cfg),
		Timeout:  agentTimeout,
		Lenguaje: config.DetectLanguage(root),
		OnUsage: func(tk loop.Tokens) {
			if presupuesto != nil {
				_ = presupuesto.Gastar(tk.Gasto())
			}
			if ventanasReg != nil {
				_ = ventanasReg.Registrar(examEx.Name(), tk.Gasto())
			}
		},
	}}

	// presupuesto: las hojas lo gastan con sus intentos; el padre
	// recursivo no, porque sus subtareas ya lo gastan cada una
	recursiva := cfg.RecursionMax > 0 && t.Recursivo
	var agente loop.Agent = agenteExecutor{exTarea}
	if recursiva {
		agente = recurse.Agent{
			Cfg:            cfg,
			Constitucion:   constitucion,
			Planificador:   generadorPlan{ex: ex, modelo: config.ModeloRol(cfg, "planificador"), root: root, effort: "medium"},
			Ejecutor:       agenteExecutor{exTarea},
			EjecutorPara:   func(m string) loop.Agent { return agenteExecutor{ejecutorPara(exTarea, m)} },
			ModeloEjecutor: modeloTarea,
			Task:           t,
			Root:           root,
			RaizID:         t.ID,
			Presupuesto:    presupuesto,
			Ventanas:       ventanasReg,
		}
	}

	opts := loop.Options{
		Agent:           agente,
		OnIntento:       progresoIntento(t.ID, modeloTarea),
		Root:            root,
		Room:            r,
		Task:            t,
		Model:           modeloTarea,
		Base:            base,
		PatronesPrueba:  patronesPruebaTarea(cfg, root, t),
		AgentTimeout:    agentTimeout,
		PruebaTimeout:   pruebaTimeout,
		Env:             []string{fmt.Sprintf("PORT=%d", r.Puerto)},
		Interfaces:      t.Usa,
		Constitucion:    constitucion,
		Skills:          skillsTarea,
		SkillsContenido: skillsContenido,
		Examinador:      exam,
		Ventana:         executor.ContextWindow(modeloTarea),
		ResumeBelowPct:  cfg.ResumeBelowPct,
	}
	if !recursiva {
		opts.Presupuesto = presupuesto
		opts.Ventanas = ventanasReg
		// el revisor solo en tareas planas: la recursión ya tiene su
		// supervisor, y revisar dos veces la misma hoja gasta doble.
		opts.Revisor = revisorParaBucle(root, cfg, ex)
		opts.RevisorVisual = revisorVisualPara(root, cfg, exTarea)
	}
	opts.Proveedor = exTarea.Name()

	outcome, err := loop.Run(ctx, opts)
	if !outcome.Verde && err == nil && executor.SinSaldo(outcome.UltimoError) {
		// sin escalar y sin detener: la tarea no falló, la cuenta no
		// tiene fondos. Pendiente, `up` la retoma al recargar.
		_ = state.Save(root, state.State{ID: t.ID, Estado: state.Pendiente, Rama: r.Rama, Puerto: r.Puerto, Commit: r.Commit, UltimoError: outcome.UltimoError})
		return runResult{ID: t.ID, Titulo: t.Titulo, Estado: estadoSinSaldo, Motivo: motivoSinSaldo(exTarea.Name(), outcome.UltimoError), Tokens: tokensDeTarea(root, t.ID)}
	}
	if !outcome.Verde && err == nil && !recursiva {
		// escalera: el modelo barato dejó trabajo y las pruebas siguen
		// rojas; se sube un escalón reusando el cuarto, sin re-examinar
		if m := cfg.ModeloEscalado(t.Peso, modeloTarea); m != "" && !outcome.NoEscalar && huboTrabajoRun(root, t.ID) {
			escalado := opts
			escalado.Model = m
			exEscalado := ejecutorPara(exTarea, m)
			escalado.Agent = agenteExecutor{exEscalado}
			escalado.Proveedor = exEscalado.Name()
			escalado.Examinador = nil
			escalado.OnIntento = progresoIntento(t.ID, m)
			outcome, err = loop.Run(ctx, escalado)
			if err == nil && outcome.Verde {
				modeloTarea = m
			}
		}
	}

	tokens := tokensDeTarea(root, t.ID)
	if err != nil {
		_ = state.Save(root, state.State{ID: t.ID, Estado: state.Detenida, Rama: r.Rama, Puerto: r.Puerto, Commit: r.Commit, UltimoError: err.Error()})
		return runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Motivo: err.Error(), Tokens: tokens}
	}

	if outcome.Verde {
		_ = state.Save(root, state.State{ID: t.ID, Estado: state.Lista, Intentos: outcome.Intentos, Rama: r.Rama, Puerto: r.Puerto, Commit: r.Commit})
		return runResult{ID: t.ID, Titulo: t.Titulo, Estado: "lista", Intentos: outcome.Intentos, Tokens: tokens}
	}
	_ = state.Save(root, state.State{
		ID: t.ID, Estado: state.Detenida, Intentos: outcome.Intentos,
		Rama: r.Rama, Puerto: r.Puerto, Commit: r.Commit,
		UltimoError: outcome.UltimoError, Pregunta: outcome.Pregunta,
	})
	return runResult{ID: t.ID, Titulo: t.Titulo, Estado: "detenida", Intentos: outcome.Intentos, Motivo: outcome.Pregunta, Tokens: tokens}
}

// estadoSinSaldo marca una tarea que no corrió o no terminó porque la
// cuenta del proveedor se quedó sin fondos. Su estado guardado sigue
// pendiente: no es un fallo suyo.
const estadoSinSaldo = "sin_saldo"

func motivoSinSaldo(cli, detalle string) string {
	return fmt.Sprintf("%s sin saldo (%s) · recarga la cuenta o cambia modelos: en .loom/config.yml · loom up retoma donde quedó", cli, strings.TrimSpace(detalle))
}

// huboTrabajoRun reporta si la tarea tocó archivos en algún intento: es
// la señal para saber si vale escalar de modelo (el código falló) o si
// escalar no cambia nada (el modelo ni escribió).
func huboTrabajoRun(root, id string) bool {
	as, err := loop.ReadAttempts(root, id)
	if err != nil {
		return false
	}
	for _, a := range as {
		if len(a.ArchivosTocados) > 0 {
			return true
		}
	}
	return false
}

// tokensDeTarea suma el gasto de todos los intentos de una tarea.
func tokensDeTarea(root, id string) int {
	as, err := loop.ReadAttempts(root, id)
	if err != nil {
		return 0
	}
	total := 0
	for _, a := range as {
		total += a.Tokens.Gasto()
	}
	return total
}

func motivoRechazo(res gate.Result) string {
	var partes []string
	for _, c := range res.Chequeos {
		if c.OK {
			continue
		}
		if c.Motivo != "" {
			partes = append(partes, c.Nombre+" · "+c.Motivo)
		} else {
			partes = append(partes, c.Nombre)
		}
	}
	if len(partes) == 0 {
		return res.PrimerMotivo()
	}
	return strings.Join(partes, " · ")
}

func sortRunResults(rs []runResult) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
}

func emitirResultados(results []runResult) error {
	if err := out.Data(results); err != nil {
		return err
	}
	var motivoSaldo string
	sinSaldo := 0
	for _, r := range results {
		switch r.Estado {
		case estadoSinSaldo:
			sinSaldo++
			if !strings.HasPrefix(r.Motivo, "no se lanzó") {
				motivoSaldo = r.Motivo
			}
		case "lista":
			extra := ""
			if r.Tokens > 0 {
				extra = " · " + budget.FormatearGasto(r.Tokens) + " tokens"
			}
			intentos := "1 intento"
			if r.Intentos != 1 {
				intentos = fmt.Sprintf("%d intentos", r.Intentos)
			}
			out.Line("✓ %s  %s  · verde en %s%s", r.ID, r.Titulo, intentos, extra)
		case "detenida":
			out.Line("⏸ %s  %s  · %s", r.ID, r.Titulo, r.Motivo)
		case "rechazada":
			out.Line("✗ %s  %s  · %s", r.ID, r.Titulo, r.Motivo)
		}
	}
	if sinSaldo > 0 {
		// una línea para todas: repetir el 402 por tarea lo hacía parecer
		// veinte fallos distintos
		if motivoSaldo == "" {
			motivoSaldo = "el proveedor no tiene saldo · recarga la cuenta y corre loom up"
		}
		out.Line("⏸ %d tareas quedaron pendientes · %s", sinSaldo, motivoSaldo)
	}
	return nil
}

// tieneDetenidas reporta si alguna tarea quedó detenida, para poder
// sugerir --retry en vez de dejar al usuario sin salida.
func tieneDetenidas(root string, tareas []task.Task) bool {
	for _, t := range tareas {
		if s, err := state.Get(root, t.ID); err == nil && s.Estado == state.Detenida {
			return true
		}
	}
	return false
}

// progresoIntento imprime el avance de una tarea cuando no hay tablero.
// En modo plano una corrida podía pasar una hora sin escribir una línea:
// el usuario no tenía forma de saber si seguía viva ni en qué iba.
func progresoIntento(id, modelo string) func(int, string) {
	if esTUI() {
		return nil // el tablero ya muestra el estado en vivo
	}
	return func(intento int, fase string) {
		switch fase {
		case "agente":
			out.Line("▸ %s  intento %d · %s trabajando", id, intento, modelo)
		case "verificando":
			out.Line("▸ %s  intento %d · verificando", id, intento)
		}
	}
}

// fasesVivas traduce los latidos de disco a lo que el tablero pinta. Es
// la única fuente que sabe qué pasa DENTRO de un intento: attempts.jsonl
// no se escribe hasta que el intento termina.
func fasesVivas(root string, ids []string) map[string]tui.FaseRun {
	latidos := loop.LeerLatidos(root, ids)
	fases := make(map[string]tui.FaseRun, len(latidos))
	for id, l := range latidos {
		fases[id] = tui.FaseRun{
			Intento:   l.Intento,
			Limite:    l.Limite,
			Fase:      l.Fase,
			Modelo:    l.Modelo,
			DesdeFase: l.DesdeFase,
			Entrada:   l.Tokens.Entrada,
			Salida:    l.Tokens.Salida,
			Atascada:  l.EnFaseDesde() >= standup.UmbralAtasco,
		}
	}
	return fases
}

// patronesPrueba devuelve las rutas que ninguna tarea puede editar.
//
// Vacío cuando el stack no tiene examinador ciego (node, rust): la regla
// existe para que quien implementa no pueda tocar el examen que lo
// juzga, y sin examinador no hay examen que proteger — solo un archivo
// de prueba que nadie puede escribir y una tarea imposible.
// patronesPruebaTarea es lo mismo por tarea. La veda tiene que decidirse
// aquí y no por lenguaje: en un repo Go, una tarea sobre `cmd/algo`
// (`package main`) no se puede examinar —Go no deja importar un main— y
// vedarle las rutas de prueba la dejaba imposible: el implementador
// escribía sus pruebas, la reversión de alcance se las quitaba y `go test
// ./cmd/algo/...` pasaba sin ejecutar nada, que el bucle ahora corta.
// Donde sí hay examen, la veda sigue intacta.
func patronesPruebaTarea(cfg config.Config, root string, t task.Task) []string {
	if !examiner.Examinable(root, t, config.DetectLanguage(root)) {
		return []string{}
	}
	if t.ExamenEsqueleto {
		var patrones []string
		if len(cfg.PatronesPrueba) > 0 {
			patrones = append(patrones, cfg.PatronesPrueba...)
		} else {
			patrones = config.DefaultTestPatterns()
		}
		// Los patrones históricos no incluyen necesariamente *.test.js/tsx.
		// La ruta exacta del examen sí debe quedar vedada siempre.
		return append(patrones, t.ExamenVisible)
	}
	return patronesPrueba(cfg, root)
}

func patronesPrueba(cfg config.Config, root string) []string {
	if !examiner.Soportado(config.DetectLanguage(root)) {
		// vacío NO nil: nil significaría "no configurado, usa los del
		// proyecto", y volveríamos a vedar rutas que nadie más escribe
		return []string{}
	}
	if len(cfg.PatronesPrueba) > 0 {
		return cfg.PatronesPrueba
	}
	return config.DefaultTestPatterns()
}
