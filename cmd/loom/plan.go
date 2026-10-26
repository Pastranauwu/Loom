package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/gate"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
)

// propuesta es una tarea del plan, ya con id asignado.
type propuesta struct {
	ID           string   `json:"id"`
	Titulo       string   `json:"titulo"`
	Porque       string   `json:"porque,omitempty"`
	ListoCuando  string   `json:"listo_cuando"`
	TocarSolo    []string `json:"tocar_solo,omitempty"`
	DependeDe    []string `json:"depende_de,omitempty"`
	Expone       []string `json:"expone,omitempty"`
	Usa          []string `json:"usa,omitempty"`
	Riesgos      string   `json:"riesgos,omitempty"`
	Peso         string   `json:"peso,omitempty"`
	Agente       string   `json:"agente,omitempty"`
	LimiteLineas int      `json:"limite_lineas"`
	Como         string   `json:"como,omitempty"`
}

func newPlanCmd() *cobra.Command {
	var modelo, ejecutor, exportSpec string
	var aprobar bool
	cmd := &cobra.Command{
		Use:   `plan "<texto>"`,
		Short: "convierte una petición en contratos de tarea",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(strings.Join(args, " "), modelo, ejecutor, exportSpec, aprobar)
		},
	}
	cmd.Flags().StringVar(&modelo, "model", "", "modelo del planificador (por defecto, el suyo)")
	cmd.Flags().StringVar(&ejecutor, "executor", "", "opencode o claude (por defecto, el primero disponible)")
	cmd.Flags().StringVar(&exportSpec, "export-spec", "", "exporta la propuesta como archivo de especificación declarativa (ej. loom.spec.yml)")
	cmd.Flags().BoolVar(&aprobar, "approve", false, "crea las tareas sin preguntar")
	return cmd
}

func runPlan(frase, modelo, ejecutor, exportSpec string, aprobar bool) error {
	root, cfg, err := entornoListo(false)
	if err != nil {
		return err
	}
	dir := config.TasksDir(root)
	if modelo == "" {
		modelo = config.ModeloRol(cfg, "planificador")
	}

	if ejecutor == "" {
		ejecutor = cfg.Cli
	}
	ex, err := elegirEjecutor(ejecutor)
	if err != nil {
		return err
	}

	ctx, zonas, patrones, err := contextoPlan(root, cfg, frase)
	if err != nil {
		return err
	}
	if ctx.EsVacio && !aprobar && isTerminal(os.Stdin) {
		ctx.Stack, ctx.Requisitos = pedirRequisitos(os.Stdin, esTUI())
	}

	var borradores []plan.Borrador
	err = esperarPlan("generando plan · "+modelo, func(avance func(string)) error {
		var err error
		borradores, err = plan.Generar(context.Background(), planGuardado{generadorPlan{ex: ex, modelo: modelo, root: root, effort: "medium", avance: avance}, root}, ctx, frase)
		return err
	})
	if err != nil {
		return err
	}

	sanearAlcance(borradores, zonas, patrones, ctx.Ocupados)
	sanearSkills(borradores, ctx.Skills)

	ids, err := idsCorrelativos(dir, len(borradores))
	if err != nil {
		return err
	}
	traducirDependencias(borradores, ids, idsPrevios(dir))
	props := make([]propuesta, len(borradores))
	for i, b := range borradores {
		props[i] = propuesta{
			ID:           ids[i],
			Titulo:       b.Titulo,
			Porque:       b.Porque,
			ListoCuando:  b.ListoCuando,
			TocarSolo:    b.TocarSolo,
			DependeDe:    b.DependeDe,
			Expone:       b.Expone,
			Usa:          b.Usa,
			Riesgos:      b.Riesgos,
			Peso:         b.Peso,
			Agente:       b.Agente,
			LimiteLineas: plan.AcotarLimiteLineas(b.LimiteLineas, task.DefaultLimiteLineas),
			Como:         b.Como,
		}
	}

	if err := out.Data(props); err != nil {
		return err
	}
	if esTUI() {
		var cuerpo strings.Builder
		cuerpo.WriteString(tui.Titulo(fmt.Sprintf("PROPONGO %d TAREAS", len(props))))
		cuerpo.WriteString("\n\n")
		for _, p := range props {
			ag := ""
			if p.Agente != "" {
				ag = " [" + p.Agente + "]"
			}
			cuerpo.WriteString(p.ID)
			cuerpo.WriteString("  ")
			cuerpo.WriteString(p.Titulo)
			cuerpo.WriteString(ag)
			cuerpo.WriteString("  ")
			cuerpo.WriteString(tui.Apagado(fmt.Sprintf("· %s · listo cuando: %s", descripcionLineas(p.LimiteLineas), p.ListoCuando)))
			cuerpo.WriteString("\n")
		}
		out.Line("%s", tui.Caja(strings.TrimRight(cuerpo.String(), "\n")))
	} else {
		out.Line(idioma.T("propongo %d tareas:"), len(props))
		for _, p := range props {
			ag := ""
			if p.Agente != "" {
				ag = " [" + p.Agente + "]"
			}
			out.Line(idioma.T("%s  %s%s  · %s · listo cuando: %s"), p.ID, p.Titulo, ag, descripcionLineas(p.LimiteLineas), p.ListoCuando)
		}
	}

	if exportSpec != "" {
		if !filepath.IsAbs(exportSpec) {
			exportSpec = filepath.Join(root, exportSpec)
		}
		var tasks []task.Task
		for i, b := range borradores {
			tasks = append(tasks, task.Task{
				Version:        task.Version,
				ID:             ids[i],
				Titulo:         b.Titulo,
				Porque:         b.Porque,
				ListoCuando:    b.ListoCuando,
				TocarSolo:      b.TocarSolo,
				NoTocar:        b.NoTocar,
				DependeDe:      b.DependeDe,
				Expone:         b.Expone,
				Usa:            b.Usa,
				Riesgos:        b.Riesgos,
				Peso:           b.Peso,
				Agente:         b.Agente,
				Skills:         b.Skills,
				Notas:          b.Como,
				LimiteIntentos: task.DefaultLimiteIntentos,
				LimiteLineas:   props[i].LimiteLineas,
			})
		}
		s := spec.Spec{
			Version: 1,
			Feature: frase,
			Tasks:   tasks,
		}
		if err := os.WriteFile(exportSpec, spec.Marshal(s), 0o644); err != nil {
			return fmt.Errorf(idioma.T("error al guardar especificación: %w"), err)
		}
		out.Line(idioma.T("✓ especificación guardada en %s · revísala y aplícala con loom apply"), exportSpec)
		if !aprobar {
			return nil
		}
	}

	// qué tareas se crean: en terminal el humano elige una por una, sin
	// terminal solo cabe todo o nada. El planificador es un modelo y
	// propone de más; aceptar el lote entero obligaba a borrar a mano
	// después, que es justo lo que loom evita.
	elegidas := map[string]bool{}
	for _, p := range props {
		elegidas[p.ID] = true
	}
	if !aprobar {
		if !isTerminal(os.Stdin) {
			out.Line(idioma.T("sin confirmación interactiva · usa --approve para crearlas"))
			return nil
		}
		if esTUI() {
			marcadas, ok, err := tui.Marcar("¿QUÉ TAREAS CREO?", "", opcionesDePlan(props))
			if err != nil {
				return err
			}
			if !ok {
				out.Line(idioma.T("plan descartado"))
				return nil
			}
			elegidas = map[string]bool{}
			for _, id := range marcadas {
				elegidas[id] = true
			}
			// descartar una tarea deja huérfanas a las que dependían de
			// ella: se quedarían "bloqueadas · depende de T-00X" para
			// siempre. Se caen con ella, y se dice cuáles.
			if arrastradas := cerrarDependencias(props, elegidas); len(arrastradas) > 0 {
				out.Line(idioma.T("· también quito %s · dependen de una tarea descartada"), strings.Join(arrastradas, ", "))
			}
			if len(elegidas) == 0 {
				out.Line(idioma.T("ninguna tarea marcada · plan descartado"))
				return nil
			}
		} else if !confirmar(os.Stdin) {
			out.Line(idioma.T("plan descartado"))
			return nil
		}
	}

	var creadas []string
	for i, b := range borradores {
		if !elegidas[ids[i]] {
			continue
		}
		creadas = append(creadas, ids[i])
		t := task.Task{
			Version:        task.Version,
			ID:             ids[i],
			Titulo:         b.Titulo,
			Porque:         b.Porque,
			ListoCuando:    b.ListoCuando,
			TocarSolo:      b.TocarSolo,
			NoTocar:        b.NoTocar,
			DependeDe:      b.DependeDe,
			Expone:         b.Expone,
			Usa:            b.Usa,
			Riesgos:        b.Riesgos,
			Peso:           b.Peso,
			Agente:         b.Agente,
			Skills:         b.Skills,
			Notas:          b.Como,
			LimiteIntentos: task.DefaultLimiteIntentos,
			LimiteLineas:   props[i].LimiteLineas,
		}
		if err := task.Save(dir, t); err != nil {
			return err
		}
	}
	if len(creadas) == 0 {
		out.Line(idioma.T("ninguna tarea creada"))
		return nil
	}
	out.Line(idioma.T("✓ %s creadas · revisa con loom check %s"), strings.Join(creadas, ", "), creadas[0])
	if len(creadas) < len(ids) {
		out.Line(idioma.T("· descartadas: %s"), strings.Join(descartadas(ids, creadas), ", "))
	}

	// `pruebas` es lo que corre el paso bisectable de la esclusa de
	// salida. Si está vacío, ship falla recién al final, cuando el
	// trabajo ya está hecho; avisar acá cuesta una línea y llega a
	// tiempo. En greenfield `init` no pudo detectarlo: no había código.
	if strings.TrimSpace(cfg.Pruebas) == "" {
		out.Line(idioma.T("· sin comando de pruebas en config.yml · decláralo antes de loom ship"))
	}
	return nil
}

// contextoPlan reúne lo que el planificador sabe del repo, y las zonas y
// patrones con los que después se sanea lo que proponga.
func contextoPlan(root string, cfg config.Config, consulta string) (ctx plan.Contexto, zonas, patrones []string, err error) {
	constitucion, err := constitucionDe(root)
	if err != nil {
		return ctx, nil, nil, err
	}
	zonas, patrones = zonasYPatronesDe(cfg, root)
	lenguaje := config.DetectLanguage(root)
	primer, err := task.NextID(config.TasksDir(root))
	if err != nil {
		return ctx, nil, nil, err
	}
	ctx = plan.Contexto{
		Lenguaje:       lenguaje,
		EsVacio:        config.DetectEmpty(root),
		Pruebas:        cfg.Pruebas,
		Constitucion:   constitucion,
		Vedadas:        append(append([]string{}, zonas...), patrones...),
		PruebasPropias: !examiner.Soportado(lenguaje),
		Agentes:        cfg.TodosLosAgentes(),
		Ocupados:       alcancesOcupados(root),
		Expuestas:      firmasExpuestas(root),
		PrimerID:       primer,
		Skills:         skills.Catalogo(root),
		Grafo:          resumenGrafo(root, consulta),
	}
	return ctx, zonas, patrones, nil
}

// zonasYPatrones devuelve las zonas prohibidas y los patrones de prueba
// efectivos: los de config.yml, o los del proyecto si config no los
// declara.
func zonasYPatrones(cfg config.Config) (zonas, patrones []string) {
	zonas = cfg.ZonasProhibidas
	if len(zonas) == 0 {
		zonas = config.DefaultForbiddenZones()
	}
	patrones = cfg.PatronesPrueba
	if len(patrones) == 0 {
		patrones = config.DefaultTestPatterns()
	}
	return zonas, patrones
}

// zonasYPatronesDe es zonasYPatrones ajustado al stack del repo: donde no
// hay examinador ciego, las rutas de prueba no son zona vedada, porque
// tiene que poder escribirlas quien implementa (ver patronesPrueba).
func zonasYPatronesDe(cfg config.Config, root string) (zonas, patrones []string) {
	zonas, patrones = zonasYPatrones(cfg)
	if !examiner.Soportado(config.DetectLanguage(root)) {
		return zonas, []string{} // vacío = ninguna vedada; nil = usa los del proyecto
	}
	return zonas, patrones
}

// sanearAlcance recorta de tocar_solo las rutas que la esclusa de
// entrada rechaza sí o sí. El planificador es un modelo y a veces las
// mete (típico: go.sum junto a go.mod); sin esto el plan entero muere
// en `loom run` y no hay arreglo salvo editar a mano.
// sanearSkills descarta las skills que el planificador nombró y no están
// en el catálogo: una inventada no tiene texto que inyectar.
func sanearSkills(bs []plan.Borrador, catalogo []skills.Skill) {
	existe := map[string]bool{}
	for _, sk := range catalogo {
		existe[sk.Nombre] = true
	}
	for i := range bs {
		if bs[i].Skills == nil {
			continue
		}
		validas := []string{}
		for _, n := range bs[i].Skills {
			if existe[n] {
				validas = append(validas, n)
			} else {
				out.Line(idioma.T("· %s: skill %q no está en .agents/skills · se omite"), bs[i].Titulo, n)
			}
		}
		bs[i].Skills = validas
	}
}

func sanearAlcance(bs []plan.Borrador, zonas, patrones []string, ocupados map[string][]string) {
	for i := range bs {
		limpio := bs[i].TocarSolo[:0]
		for _, p := range bs[i].TocarSolo {
			if z, mal := gate.AlcanceProhibido(p, zonas, patrones); mal {
				out.Line(idioma.T("· quito %q de tocar_solo · zona vedada (%s)"), p, z)
				continue
			}
			if id, dueño := alcanceOcupado(p, ocupados); dueño {
				// no se recorta: un alcance que se cruza suele ser el
				// corazon de la tarea, y quitarlo la deja sin sentido.
				// Se avisa para que el humano parta o espere.
				out.Line(idioma.T("⚠ %q se cruza con %s, que está en curso · la esclusa la rechazará hasta que %s termine"), p, id, id)
			}
			limpio = append(limpio, p)
		}
		bs[i].TocarSolo = limpio
	}
}

// ampliarPruebasPropias garantiza que en un stack sin examinador ciego
// (node, rust) la tarea pueda escribir la suite que su propio
// listo_cuando exige. El planificador es un modelo: apunta el comando a
// un archivo de prueba (`npx vitest run src/core/types`) y deja ese
// archivo fuera de tocar_solo, convencido de que un examinador lo
// escribirá. No hay examinador: la reversión de alcance le quita al
// agente la suite que intente crear y la tarea queda roja para siempre,
// quemando los tres intentos. Aquí el comando se mira de forma
// determinista: cada path de prueba que mencione entra al alcance.
func ampliarPruebasPropias(bs []plan.Borrador) {
	for i := range bs {
		for _, path := range task.ArchivosDePrueba(bs[i].ListoCuando) {
			if config.MatchesAny(bs[i].TocarSolo, path) {
				continue
			}
			out.Line(idioma.T("· %s · agrego %q a tocar_solo · sin examinador, la tarea escribe su propia suite"), bs[i].Titulo, path)
			bs[i].TocarSolo = append(bs[i].TocarSolo, path)
		}
	}
}

// alcancesOcupados devuelve tocar_solo de las tareas en curso, por id.
// Es lo que el planificador necesita para no proponer trabajo que la
// esclusa de entrada va a rechazar.
func alcancesOcupados(root string) map[string][]string {
	estados, err := state.List(root)
	if err != nil {
		return nil
	}
	enCurso := map[string]bool{}
	for _, st := range estados {
		if st.Estado == state.EnCurso {
			enCurso[st.ID] = true
		}
	}
	if len(enCurso) == 0 {
		return nil
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return nil
	}
	ocupados := map[string][]string{}
	for _, t := range tareas {
		if enCurso[t.ID] && len(t.TocarSolo) > 0 {
			ocupados[t.ID] = t.TocarSolo
		}
	}
	if len(ocupados) == 0 {
		return nil
	}
	return ocupados
}

// firmasExpuestas devuelve el expone de las tareas que ya hay en el
// repo, por id: las mismas que ValidatePlan usa de referencia.
func firmasExpuestas(root string) map[string][]string {
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return nil
	}
	out := map[string][]string{}
	for _, t := range tareas {
		if len(t.Expone) > 0 {
			out[t.ID] = t.Expone
		}
	}
	return out
}

// alcanceOcupado reporta si p se cruza con el alcance de alguna tarea en
// curso, y con cuál.
func alcanceOcupado(p string, ocupados map[string][]string) (string, bool) {
	for id, globs := range ocupados {
		for _, g := range globs {
			if gate.GlobsSeCruzan(p, g) {
				return id, true
			}
		}
	}
	return "", false
}

// idsCorrelativos devuelve n ids libres a partir del primero.
func idsCorrelativos(dir string, n int) ([]string, error) {
	first, err := task.NextID(dir)
	if err != nil {
		return nil, err
	}
	num, err := strconv.Atoi(strings.TrimPrefix(first, "T-"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("T-%03d", num+i)
	}
	return ids, nil
}

// traducirDependencias deja los ids del plan y los de tareas previas tal
// cual (el prompt le dice al modelo desde qué id numera) y lee por
// posición solo lo que no reconoce. Leerlo todo por posición convertía
// el "T-005" real de un modelo que numeraba desde T-002 en la quinta
// tarea, y el plan salía lleno de ciclos.
func traducirDependencias(bs []plan.Borrador, ids []string, previas map[string]bool) {
	for i := range bs {
		deps := dependenciasDelModelo(bs[i].DependeDe, ids, previas)
		// la tarea final "depende de todas" suele incluirse a sí misma
		// (T-064 → T-064): nunca es intencional y arma un ciclo
		bs[i].DependeDe = deps[:0]
		for _, d := range deps {
			if i >= len(ids) || d != ids[i] {
				bs[i].DependeDe = append(bs[i].DependeDe, d)
			}
		}
	}
}

// ciclo devuelve una dependencia circular del plan (ya traducido a ids),
// o nada. Se busca antes de aceptar el plan para que la corrija quien
// lo escribió, no después, cuando ValidatePlan tira un plan pagado.
func ciclo(bs []plan.Borrador, ids []string) []string {
	deps := map[string][]string{}
	for i, b := range bs {
		if i < len(ids) {
			deps[ids[i]] = b.DependeDe
		}
	}
	estado := map[string]int{} // 1 visitando, 2 listo
	var camino []string
	var visitar func(string) []string
	visitar = func(id string) []string {
		switch estado[id] {
		case 1:
			for k, c := range camino {
				if c == id {
					return append(append([]string{}, camino[k:]...), id)
				}
			}
		case 2:
			return nil
		}
		estado[id] = 1
		camino = append(camino, id)
		for _, d := range deps[id] {
			if c := visitar(d); c != nil {
				return c
			}
		}
		camino = camino[:len(camino)-1]
		estado[id] = 2
		return nil
	}
	for _, id := range ids {
		if c := visitar(id); c != nil {
			return c
		}
	}
	return nil
}

// idsPrevios devuelve los ids de las tareas que ya hay en dir.
func idsPrevios(dir string) map[string]bool {
	tareas, _ := task.List(dir)
	out := make(map[string]bool, len(tareas))
	for _, t := range tareas {
		out[t.ID] = true
	}
	return out
}

// confirmar pregunta s/n y devuelve si el usuario aprobó.
func confirmar(in io.Reader) bool {
	out.Line(idioma.T("¿crear estas tareas? [s/n]"))
	linea, _ := bufio.NewReader(in).ReadString('\n')
	r := strings.ToLower(strings.TrimSpace(linea))
	return r == "s" || r == "si" || r == "y" || r == "yes"
}

// pedirRequisitos reúne el stack y los requisitos extra del humano
// cuando el repo está vacío: sin esto, el planificador no
// tiene de dónde agarrarse y alucina un stack.
func pedirRequisitos(in io.Reader, tuiMode bool) (stack, requisitos string) {
	leer := bufio.NewReader(in)

	titulo, etiquetaStack, etiquetaPieza := "repositorio vacío · define el stack y los requisitos antes de planear",
		"stack (go, node, python, rust, ...) · enter para que lo elija el modelo:",
		"requisito o pieza (una línea, enter para terminar):"
	if tuiMode {
		titulo, etiquetaStack, etiquetaPieza = tui.Titulo(titulo), tui.Apagado(etiquetaStack), tui.Apagado(etiquetaPieza)
	}

	out.Line("%s", titulo)
	out.Line("%s", etiquetaStack)
	if l, _ := leer.ReadString('\n'); strings.TrimSpace(l) != "" {
		stack = strings.ToLower(strings.TrimSpace(l))
	}

	var piezas []string
	for {
		out.Line("%s", etiquetaPieza)
		l, _ := leer.ReadString('\n')
		l = strings.TrimSpace(l)
		if l == "" {
			break
		}
		piezas = append(piezas, l)
	}
	return stack, strings.Join(piezas, "; ")
}

// generadorPlan adapta el ejecutor al generador de texto del planificador.
// effort vacío deja que el CLI decida: el planificador lo sube a medio a
// propósito (la arquitectura merece pensar más); el revisor barato no, un
// `--effort` que su modelo no soporta lo rompería.
type generadorPlan struct {
	ex     executor.Executor
	modelo string
	root   string
	effort string
	avance func(string) // nil: sin avances
}

func (g generadorPlan) Generar(ctx context.Context, prompt string) (string, error) {
	timeout := 20 * time.Minute
	if cfg, err := config.Load(g.root); err == nil && cfg.TimeoutAgente > 0 {
		timeout = time.Duration(cfg.TimeoutAgente) * time.Second
	}
	res, err := g.ex.Run(ctx, executor.Request{
		Rol:      executor.RolPlanificador,
		RoomPath: g.root,
		Prompt:   prompt,
		Model:    g.modelo,
		Timeout:  timeout,
		Effort:   g.effort,
		Avance:   g.avance,
	})
	if err != nil {
		if res.ExitCode == 124 {
			return "", fmt.Errorf(idioma.T("el planificador agotó %s · ajusta timeout_agente en .loom/config.yml"), timeout)
		}
		return "", err
	}
	return res.Text, nil
}

// esperarPlan corre el planificador mostrando el tiempo que lleva y sus
// avances: en la TUI bajo el spinner, en --plain como líneas.
func esperarPlan(titulo string, trabajo func(avance func(string)) error) error {
	if esTUI() {
		return tui.EsperarConAvances(titulo, trabajo)
	}
	out.Line("· %s", titulo)
	inicio := time.Now()
	return trabajo(func(s string) { out.Line("  %s · %s", tui.Duracion(time.Since(inicio)), s) })
}

// planGuardado reusa la respuesta cruda del planificador mientras el
// prompt sea idéntico, es decir, mientras ningún contrato se haya escrito
// (PrimerID y Expuestas cambian en cuanto se escribe uno). Un plan
// pagado que falló por parseo o validación se recompone con el código
// arreglado en vez de replanearse desde cero.
type planGuardado struct {
	gen  plan.Generador
	root string
}

func (g planGuardado) Generar(ctx context.Context, prompt string) (string, error) {
	ruta := filepath.Join(config.Dir(g.root), "plan-crudo.json")
	suma := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
	var guardado struct{ Prompt, Texto string }
	if b, err := os.ReadFile(ruta); err == nil && json.Unmarshal(b, &guardado) == nil && guardado.Prompt == suma {
		out.Line(idioma.T("· reusando la respuesta guardada del planificador · borra .loom/plan-crudo.json para replanear"))
		return guardado.Texto, nil
	}
	texto, err := g.gen.Generar(ctx, prompt)
	if err == nil && strings.TrimSpace(texto) != "" {
		guardado.Prompt, guardado.Texto = suma, texto
		if b, errJSON := json.Marshal(guardado); errJSON == nil {
			_ = os.WriteFile(ruta, b, 0o644)
		}
	}
	return texto, err
}

// opcionesDePlan arma la lista que ve el humano antes de aprobar. El
// detalle lleva lo que decide si una tarea vale: por qué, cómo se
// comprueba y qué archivos toca.
func opcionesDePlan(props []propuesta) []tui.Opcion {
	ops := make([]tui.Opcion, 0, len(props))
	for _, p := range props {
		etiqueta := p.ID + "  " + p.Titulo
		if p.Agente != "" {
			etiqueta += "  [" + p.Agente + "]"
		}
		detalle := fmt.Sprintf("listo cuando: %s · %s", p.ListoCuando, descripcionLineas(p.LimiteLineas))
		if len(p.TocarSolo) > 0 {
			detalle += " · toca: " + strings.Join(p.TocarSolo, ", ")
		}
		if len(p.DependeDe) > 0 {
			detalle += " · depende de: " + strings.Join(p.DependeDe, ", ")
		}
		ops = append(ops, tui.Opcion{ID: p.ID, Etiqueta: etiqueta, Detalle: detalle, Marcada: true})
	}
	return ops
}

// descartadas devuelve los ids propuestos que el humano no marcó.
func descartadas(todos, creados []string) []string {
	hecho := make(map[string]bool, len(creados))
	for _, id := range creados {
		hecho[id] = true
	}
	var fuera []string
	for _, id := range todos {
		if !hecho[id] {
			fuera = append(fuera, id)
		}
	}
	return fuera
}

// cerrarDependencias quita de `elegidas` toda tarea que dependa de una
// descartada, en cascada. Devuelve las que se cayeron por arrastre.
//
// Sin esto, descartar una tarea del plan producía un plan imposible: sus
// dependientes se creaban igual y `run` las rechazaba en cada corrida con
// "bloqueada · depende de T-00X que no salió verde", sin más arreglo que
// editar los contratos a mano.
func cerrarDependencias(props []propuesta, elegidas map[string]bool) []string {
	propuestas := make(map[string]bool, len(props))
	for _, p := range props {
		propuestas[p.ID] = true
	}

	var arrastradas []string
	for {
		cayo := false
		for _, p := range props {
			if !elegidas[p.ID] {
				continue
			}
			for _, d := range p.DependeDe {
				// una dependencia fuera del plan ya existe en el repo
				if propuestas[d] && !elegidas[d] {
					delete(elegidas, p.ID)
					arrastradas = append(arrastradas, p.ID)
					cayo = true
					break
				}
			}
		}
		if !cayo {
			return arrastradas
		}
	}
}

func descripcionLineas(limite int) string {
	if limite == 0 {
		return "sin límite de líneas"
	}
	return fmt.Sprintf("límite de %d líneas", limite)
}
