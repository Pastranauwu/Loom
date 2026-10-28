package main

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/recurse"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

type boardRow struct {
	ID         string     `json:"id"`
	Titulo     string     `json:"titulo"`
	Estado     string     `json:"estado"`
	Detalle    string     `json:"detalle,omitempty"`
	Weight     string     `json:"weight,omitempty"`
	Exposes    []string   `json:"exposes,omitempty"`
	MaxRetries int        `json:"max_retries,omitempty"`
	Hijos      []boardRow `json:"hijos,omitempty"`
}

func newBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "board",
		Short: "tablero de estado",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBoard()
		},
	}
}

func runBoard() error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	if esTUI() {
		accion, err := tui.CorrerBoard(root)
		if err != nil {
			return err
		}
		// el tablero solo decide; lo que corre lo corre el comando, para
		// que la salida sea la misma que si se hubiera tecleado
		switch accion.Tipo {
		case tui.AccionEntregar:
			return runShip(accion.ID, true, false)
		case tui.AccionReintentar:
			return reintentarTarea(root, accion.ID)
		case tui.AccionEntregarTodas:
			return runShipTodas(false, "", false, false, false)
		}
		return nil
	}

	tasks, err := task.List(config.TasksDir(root))
	if err != nil {
		return err
	}

	var lista, enCurso, detenidas, pendientes []boardRow
	for _, t := range tasks {
		s, err := state.Get(root, t.ID)
		if err != nil {
			return err
		}
		nodos, err := recurse.LeerArbol(root, t.ID)
		if err != nil {
			return err
		}
		row := boardRow{ID: t.ID, Titulo: t.Titulo, Estado: s.Estado, Hijos: hijosBoardRow(root, nodos, t.ID),
			Weight: task.PesoEn(t.Peso), Exposes: t.Expone, MaxRetries: t.LimiteIntentos}
		// una tarea en curso puede tener encima una corrida muerta: el
		// estado en disco dice `en_curso` para siempre porque nadie lo
		// bajó. Decirlo es el punto del tablero — antes se pintaba igual
		// que una tarea viva.
		if s.Estado == state.EnCurso {
			if l, corriendo := loop.LeerLatido(root, t.ID); corriendo {
				row.Detalle = l.Descripcion() + " · " + l.EnFaseDesde().Round(time.Second).String()
			} else if l, muerta := loop.Interrumpida(root, t.ID); muerta {
				row.Detalle = fmt.Sprintf("interrumpida · sin señal hace %s · loom run --retry", l.Silencio().Round(time.Second))
			}
		}
		switch s.Estado {
		case state.Lista:
			lista = append(lista, row)
		case state.EnCurso:
			enCurso = append(enCurso, row)
		case state.Detenida:
			detenidas = append(detenidas, row)
		default:
			pendientes = append(pendientes, row)
		}
	}
	sort.Slice(lista, byID(lista))
	sort.Slice(enCurso, byID(enCurso))
	sort.Slice(detenidas, byID(detenidas))
	sort.Slice(pendientes, byID(pendientes))

	// un solo Data para el modo JSON
	todo := map[string][]boardRow{
		"listo":     lista,
		"en_curso":  enCurso,
		"detenido":  detenidas,
		"pendiente": pendientes,
	}
	if err := out.Data(todo); err != nil {
		return err
	}

	// barra de presupuesto si la corrida tiene tope
	cfg, err := config.Load(root)
	if err == nil {
		if cfg.PresupuestoTokens > 0 {
			out.Line(idioma.T("PRESUPUESTO %s"), budget.Barra(budget.GastoEnDisco(root), cfg.PresupuestoTokens))
		}
		ventanasReg := ventanas.Nuevo(ventanas.LedgerPath(), cfg.PresupuestoVentanas)
		for _, p := range config.Clis {
			if l := ventanas.LineaVentanas(ventanasReg, p); l != "" {
				out.Line(idioma.T("PRESUPUESTO %s"), l)
			}
		}
		if cfg.PresupuestoTokens > 0 || len(cfg.PresupuestoVentanas) > 0 {
			out.Line("")
		}
	}

	if len(tasks) == 0 {
		out.Line(idioma.T("sin tareas · empieza con loom task add \"lo que necesitas\""))
		return nil
	}
	imprimirGrupo("LISTO PARA ENTREGAR", lista)
	imprimirGrupo("EN CURSO", enCurso)
	imprimirGrupo("DETENIDO", detenidas)
	imprimirGrupo("PENDIENTE", pendientes)
	return nil
}

func byID(rows []boardRow) func(i, j int) bool {
	return func(i, j int) bool { return rows[i].ID < rows[j].ID }
}

// hijosBoardRow arma recursivamente el árbol de un padre a partir de los
// nodos planos de arbol.json. Una subtarea con latido vivo — que
// corre ahora mismo dentro de la recursión — se marca en curso y muestra
// en Detalle lo que está haciendo (intento, fase, modelo, tiempo).
func hijosBoardRow(root string, nodos []recurse.NodoArbol, padre string) []boardRow {
	var hijos []boardRow
	for _, n := range nodos {
		if n.Padre != padre {
			continue
		}
		estado := state.Detenida
		if n.Verde {
			estado = state.Lista
		}
		r := boardRow{ID: n.ID, Titulo: n.Titulo, Estado: estado, Hijos: hijosBoardRow(root, nodos, n.ID)}
		if l, corriendo := loop.LeerLatido(root, n.ID); corriendo {
			r.Estado = state.EnCurso
			r.Detalle = l.Descripcion() + " · " + l.EnFaseDesde().Round(time.Second).String()
		}
		hijos = append(hijos, r)
	}
	sort.Slice(hijos, byID(hijos))
	return hijos
}

func imprimirGrupo(nombre string, rows []boardRow) {
	if len(rows) == 0 {
		out.Line("%-20s —", nombre)
		return
	}
	for _, r := range rows {
		out.Line("%-20s %s  %s", nombre, r.ID, r.Titulo)
		nombre = ""
		if r.Detalle != "" {
			out.Line("%-20s     %s", "", r.Detalle)
		}
		imprimirHijos(r.Hijos, 1)
	}
}

// imprimirHijos imprime el árbol de subtareas indentado debajo de su
// padre, recursivo (una subtarea que a su vez recursó también sale
// anidada). Una subtarea en curso muestra su detalle vivo debajo.
func imprimirHijos(rows []boardRow, profundidad int) {
	sangria := strings.Repeat("  ", profundidad)
	for _, r := range rows {
		out.Line("%-20s %s└ %s  %s", "", sangria, r.ID, r.Titulo)
		if r.Detalle != "" {
			out.Line("%-20s %s    %s", "", sangria, r.Detalle)
		}
		imprimirHijos(r.Hijos, profundidad+1)
	}
}

// reintentarTarea vuelve a correr una sola tarea detenida, reusando su
// cuarto y el trabajo parcial que quedó dentro. Es la tecla r del
// tablero: sin esto, revivir una tarea obligaba a correr `run
// --retry` sobre todas.
func reintentarTarea(root, id string) error {
	st, err := state.Get(root, id)
	if err != nil {
		return err
	}
	if st.Estado != state.Detenida {
		return fmt.Errorf(idioma.T("%s no está detenida (estado %s) · nada que reintentar"), id, st.Estado)
	}
	// volver a pendiente es lo que hace que `run` la recoja; el cuarto y
	// su rama siguen ahí, así que el agente arranca viendo lo que ya
	// escribió en vez de rehacerlo y pagar los mismos tokens otra vez
	if err := state.Save(root, state.State{ID: id, Estado: state.Pendiente, Rama: st.Rama, Puerto: st.Puerto, Commit: st.Commit}); err != nil {
		return err
	}
	out.Line(idioma.T("· %s vuelve a la cola · se reusa su cuarto y su trabajo parcial"), id)
	return runCmd(1, "", "", true, false)
}
