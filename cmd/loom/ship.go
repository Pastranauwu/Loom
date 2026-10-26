package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/metrics"
	"github.com/Pastranauwu/loom/internal/revisor"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newShipCmd() *cobra.Command {
	var dryRun bool
	var todas bool
	var titulo string
	var integrar, revisar, local bool
	cmd := &cobra.Command{
		Use:   "ship [id]",
		Short: "esclusa de salida y PR",
		Long: `Con un id entrega esa tarea en su propio PR.

Con --all entrega en UN solo PR todas las tareas que quedaron listas:
cada una pasa su esclusa de salida por separado y aporta exactamente un
commit, en orden de dependencia. Es la forma de no terminar con N pull
requests que se pisan entre sí.`,
		Example: `  loom ship T-001
  loom ship --all
  loom ship --all --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if todas {
				if len(args) > 0 {
					return errors.New("--all entrega todas las tareas listas · no lleva id")
				}
				return runShipTodas(dryRun, titulo, integrar, revisar, local)
			}
			if len(args) == 0 {
				return errors.New("falta el id · usa loom ship T-001 o loom ship --all")
			}
			return runShip(args[0], dryRun, local)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "corre la esclusa sin abrir el PR")
	cmd.Flags().BoolVar(&todas, "all", false, "entrega todas las tareas listas en un solo PR")
	cmd.Flags().StringVar(&titulo, "title", "", "título del PR conjunto (por defecto, el de la primera tarea)")
	cmd.Flags().BoolVar(&revisar, "review", false, "un modelo revisa el diff y deja el informe en el PR; apruebas tú")
	cmd.Flags().BoolVar(&integrar, "merge", false, "revisa y, si no pide cambios, mergea el PR por rebase sin esperarte")
	cmd.Flags().BoolVar(&local, "local", false, "deja el PR en el repo (rama y descripción) aunque haya remoto; igual que entrega: local en config.yml")
	return cmd
}

// hashDelSpec es el hash de la intención del spec del repo, o "" si no hay
// spec legible (una petición suelta con `up "<frase>"` no deja archivo).
func hashDelSpec(root string) string {
	path, err := spec.Find(root)
	if err != nil {
		return ""
	}
	s, err := spec.Load(path)
	if err != nil {
		return ""
	}
	return spec.IntencionDe(s).Hash()
}

// runShipTodas entrega en un solo PR todas las tareas que quedaron
// listas. Cada una pasa su propia esclusa de salida antes de integrarse:
// el PR conjunto no baja el listón, solo evita repartirlo en N PRs que
// después hay que mergear en el orden correcto a mano.
func runShipTodas(dryRun bool, titulo string, integrar, revisar, local bool) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	soltar, err := tomarEntrega(root)
	if err != nil {
		return err
	}
	defer soltar()
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if local {
		cfg.Entrega = config.EntregaLocal
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return err
	}

	// lo que ya está en la base se da por entregado: se libera su cuarto
	// y su rama, que es como el resto de loom reconoce una tarea
	// entregada (sembrarVerdesPrevias, run)
	ctx := context.Background()
	entregadas := ship.Entregadas(ctx, root, cfg.Base)
	var listas []task.Task
	var pendientes, detenidas []string
	modelos := map[string]string{}
	commits := map[string]string{}
	for _, t := range tareas {
		st, err := state.Get(root, t.ID)
		if err != nil {
			return err
		}
		switch st.Estado {
		case state.Lista:
			if entregadas[t.ID] {
				_ = room.Destroy(ctx, root, t.ID)
				continue
			}
			if !room.RamaExiste(ctx, root, t.ID) {
				continue // entregada en una corrida anterior
			}
			listas = append(listas, t)
			modelos[t.ID] = ultimoModelo(root, t.ID)
			commits[t.ID] = st.Commit
		case state.Detenida:
			detenidas = append(detenidas, t.ID)
		case state.Pendiente, state.EnCurso:
			pendientes = append(pendientes, t.ID)
		}
	}
	if len(listas) == 0 {
		return errors.New("ninguna tarea está lista sin entregar · corre loom run primero")
	}
	ids := make([]string, 0, len(listas))
	for _, t := range listas {
		ids = append(ids, t.ID)
	}
	pruebasDeLosCuartos(root, &cfg, ids)
	// entregar la mitad de un plan deja el PR incoherente: las tareas que
	// faltan son justo las que otras consumen
	if len(detenidas) > 0 || len(pendientes) > 0 {
		var partes []string
		if len(detenidas) > 0 {
			partes = append(partes, "detenidas: "+strings.Join(detenidas, ", ")+" (revívelas con loom run --retry)")
		}
		if len(pendientes) > 0 {
			partes = append(partes, "sin correr: "+strings.Join(pendientes, ", "))
		}
		return fmt.Errorf("no todas las tareas están listas · %s", strings.Join(partes, " · "))
	}

	opciones := ship.OpcionesEntrega{
		Root:    root,
		Config:  cfg,
		Base:    cfg.Base,
		Tareas:  listas,
		Modelos: modelos,
		Commits: commits,
		Titulo:  titulo,
		DryRun:  dryRun,
		Spec:    hashDelSpec(root),
		Progreso: func(p ship.Paso) {
			if p.OK {
				out.Line("✓ %s  · %s", p.Nombre, p.Detalle)
			} else {
				out.Line("✗ %s  · %s", p.Nombre, p.Detalle)
			}
		},
	}
	if feature, err := spec.LoadFeatureState(root); err == nil {
		opciones.Acceptance = feature.AcceptanceCommands()
		// sin título, el del feature: el de la primera tarea es el
		// esqueleto ("esqueleto · <feature>…")
		if opciones.Titulo == "" {
			opciones.Titulo = feature.Feature
		}
	}
	if cfg.TimeoutPruebas > 0 {
		opciones.Timeout = time.Duration(cfg.TimeoutPruebas) * time.Second
	}
	if integrar || revisar {
		if dryRun {
			return errors.New("--review e --merge necesitan un PR donde dejar el informe · quita --dry-run")
		}
		rev, err := revisorDelProyecto(root, cfg)
		if err != nil {
			return err
		}
		opciones.Revisor, opciones.Integrar = rev, integrar
	}

	// se dice antes de las esclusas, no al abrir el PR: la entrega sigue
	// y queda como PR local
	if cfg.Entrega != config.EntregaLocal && !dryRun {
		if motivo := ship.AccesoRemoto(root); motivo != "" {
			out.Line("· %s · si no, el PR queda local", motivo)
		}
	}
	e := ship.EntregarTodas(context.Background(), opciones)
	if err := out.Data(e); err != nil {
		return err
	}
	if !e.Aprobado {
		if e.Fallo != nil {
			if ids := responsables(listas, e.Fallo.Pruebas); len(ids) > 0 {
				out.Line("· probable responsable: %s (toca el código que esa prueba cubre) · reábrela con loom reparar", strings.Join(ids, ", "))
			}
		}
		return fmt.Errorf("entrega frenada · %s", e.PrimerMotivo())
	}
	if dryRun {
		out.Line(idioma.T("entregable · %d tareas en la rama %s · --dry-run, sin PR"), len(listas), e.Rama)
		return nil
	}
	if err := ship.RegistrarEntrega(root, e.Rama, ids); err != nil {
		out.Line("· no se pudo registrar la entrega · %s · el próximo ship puede volver a incluir estas tareas", err)
	}
	if e.Integrado {
		out.Line("integrado · %d tareas en %s · %s", len(listas), cfg.Base, e.PR)
		return nil
	}
	out.Line("entregado · %d tareas en un PR · %s", len(listas), e.PR)
	if opciones.Revisor != nil {
		out.Line("· el informe del revisor está en el PR · la aprobación es tuya")
	}
	siguientePasoLocal(e.PR, e.Rama, cfg.Base)
	return nil
}

// siguientePasoLocal dice cómo revisar e integrar un PR local, que no
// tiene una página donde darle "merge".
func siguientePasoLocal(pr, rama, base string) {
	if !strings.HasPrefix(pr, ship.PRLocal) {
		return
	}
	out.Line("· revisa · git log -p %s..%s", base, rama)
	out.Line("· integra · git merge --ff-only %s", rama)
}

// revisorAgente adapta el ejecutor al generador de texto del revisor,
// igual que hace el planificador.
type revisorAgente struct{ gen generadorPlan }

func (r revisorAgente) Revisar(ctx context.Context, diff string, tareas []task.Task) (bool, string, string, error) {
	v, err := revisor.Revisar(ctx, revisor.Opciones{
		Generador: r.gen,
		Tareas:    tareas,
		Diff:      diff,
	})
	if err != nil {
		return false, "", "", err
	}
	return v.Aprobado(), v.Informe(), v.Resumen(), nil
}

// revisorDelProyecto arma el revisor con el modelo del rol `revisor`, o
// el del media/liviana si no se declaró uno propio. El rol lleva
// declarado en config desde el principio y hasta ahora no lo usaba nadie.
func revisorDelProyecto(root string, cfg config.Config) (ship.Revisor, error) {
	ex, err := elegirEjecutor(cfg.Cli)
	if err != nil {
		return nil, err
	}
	modelo := config.ModeloRol(cfg, "revisor")
	if modelo == "" {
		modelo = cfg.ModeloPeso("media")
	}
	if modelo == "" {
		modelo = cfg.ModeloPeso("liviana")
	}
	return revisorAgente{gen: generadorPlan{ex: ex, modelo: modelo, root: root}}, nil
}

func runShip(id string, dryRun, local bool) error {
	if err := validTaskID(id); err != nil {
		return err
	}
	root, err := projectRoot()
	if err != nil {
		return err
	}
	soltar, err := tomarEntrega(root)
	if err != nil {
		return err
	}
	defer soltar()
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if local {
		cfg.Entrega = config.EntregaLocal
	}
	t, err := task.Load(config.TasksDir(root), id)
	if err != nil {
		return err
	}
	pruebasDeLosCuartos(root, &cfg, []string{id})
	st, err := state.Get(root, id)
	if err != nil {
		return err
	}
	if st.Estado != state.Lista {
		return errorShipNoLista(id, st)
	}

	// una tarea suelta se aplana contra la base: si depende de otra que
	// todavía no está ahí, su commit se llevaría el trabajo de esa (el
	// esqueleto entero) bajo su nombre
	ctx := context.Background()
	entregadas := ship.Entregadas(ctx, root, cfg.Base)
	var faltan []string
	for _, dep := range t.DependeDe {
		if !entregadas[dep] && room.RamaExiste(ctx, root, dep) {
			faltan = append(faltan, dep)
		}
	}
	if len(faltan) > 0 {
		return fmt.Errorf("%s depende de %s, que todavía no se entregó · entrégalas juntas con loom ship --all", id, strings.Join(faltan, ", "))
	}

	r := room.Room{ID: id, Path: filepath.Join(room.Dir(root), id), Rama: room.Branch(id)}
	if _, err := os.Stat(r.Path); err != nil {
		return fmt.Errorf("no existe el cuarto de %s · la tarea no corrió", id)
	}

	opciones := ship.Opciones{
		Root:   root,
		Room:   r,
		Task:   t,
		Config: cfg,
		Modelo: ultimoModelo(root, id),
		Base:   cfg.Base,
		DryRun: dryRun,
		// en seco la rama queda como estaba
		Conservar: dryRun,
	}
	if cfg.TimeoutPruebas > 0 {
		opciones.Timeout = time.Duration(cfg.TimeoutPruebas) * time.Second
	}

	var res ship.Resultado
	if esTUI() {
		res, err = tui.CorrerGate(opciones)
		if err != nil {
			return err
		}
	} else {
		res = ship.Run(context.Background(), opciones)
	}

	// la entrega alimenta las métricas de ruido y roce, aunque frene
	if !dryRun {
		_ = metrics.GuardarEntrega(root, metrics.Entrega{
			ID:          id,
			Fecha:       time.Now().UTC(),
			LineasMas:   res.LineasMas,
			LineasMenos: res.LineasMenos,
			Ruido:       res.Ruido,
			Conflicto:   res.Conflicto,
			PR:          res.PR,
			Aprobado:    res.Aprobado,
			Brecha:      res.Brecha,
		})
	}

	if err := out.Data(res); err != nil {
		return err
	}
	if esTUI() {
		// la compuerta ya mostró los pasos; queda solo la línea final
		if !res.Aprobado {
			return fmt.Errorf("esclusa frenada · %s", res.PrimerMotivo())
		}
		if dryRun {
			out.Line(idioma.T("entregable · --dry-run, sin PR"))
		} else {
			out.Line("entregado · %s", res.PR)
			siguientePasoLocal(res.PR, r.Rama, cfg.Base)
		}
		return nil
	}
	for _, p := range res.Pasos {
		if p.OK {
			if p.Detalle != "" {
				out.Line("✓ %s  · %s", p.Nombre, p.Detalle)
			} else {
				out.Line("✓ %s", p.Nombre)
			}
		} else {
			out.Line("✗ %s  · %s", p.Nombre, p.Detalle)
		}
	}
	if !res.Aprobado {
		return fmt.Errorf("esclusa frenada · %s", res.PrimerMotivo())
	}
	if dryRun {
		out.Line(idioma.T("entregable · --dry-run, sin PR"))
	} else {
		out.Line("entregado · %s", res.PR)
		siguientePasoLocal(res.PR, r.Rama, cfg.Base)
	}
	return nil
}

func errorShipNoLista(id string, st state.State) error {
	switch st.Estado {
	case state.Detenida:
		razon := st.Pregunta
		if razon == "" {
			razon = st.UltimoError
		}
		if razon == "" {
			razon = "agotó intentos"
		}
		return fmt.Errorf("%s está detenida · %s · sube limite_intentos, revisa con loom logs %s o edita la tarea", id, razon, id)
	case state.EnCurso:
		return fmt.Errorf("%s sigue en curso · espera a que termine o mira loom board", id)
	case state.Pendiente:
		return fmt.Errorf("%s está pendiente · corre loom run primero", id)
	default:
		return fmt.Errorf("%s no está lista (estado %s) · corre loom run primero", id, st.Estado)
	}
}

// ultimoModelo devuelve el modelo del último intento, para el trailer
// Agent: del commit.
func ultimoModelo(root, id string) string {
	attempts, err := loop.ReadAttempts(root, id)
	if err != nil || len(attempts) == 0 {
		return ""
	}
	return attempts[len(attempts)-1].Modelo
}

// pruebasDeLosCuartos detecta el comando de pruebas en los cuartos de
// las tareas cuando config.yml no lo tiene. En un repo que arrancó vacío
// la preparación no tenía nada que detectar (el package.json o el go.mod
// lo escriben los agentes), y la entrega se frenaba al final por un
// comando que ya era obvio. Se guarda para las siguientes corridas.
func pruebasDeLosCuartos(root string, cfg *config.Config, ids []string) {
	if strings.TrimSpace(cfg.Pruebas) != "" {
		return
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if p, ok := config.DetectTestCommand(filepath.Join(room.Dir(root), ids[i])); ok {
			cfg.Pruebas = p
			if cfg.Save(root) == nil {
				out.Line("· comando de pruebas detectado en %s: %s · guardado en config.yml", ids[i], p)
			}
			return
		}
	}
}
