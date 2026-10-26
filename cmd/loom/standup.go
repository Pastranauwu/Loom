package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/recurse"
	"github.com/Pastranauwu/loom/internal/standup"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

func newStandupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "standup",
		Short: "parte de datos de las tareas en curso",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStandup()
		},
	}
}

func runStandup() error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return err
	}
	estados, err := state.List(root)
	if err != nil {
		return err
	}
	estadosPorID := make(map[string]state.State, len(estados))
	var nActivas int
	for _, s := range estados {
		estadosPorID[s.ID] = s
		if s.Estado == state.EnCurso {
			nActivas++
		}
	}
	attempts := make(map[string][]loop.Attempt, len(tareas))
	for _, t := range tareas {
		as, err := loop.ReadAttempts(root, t.ID)
		if err != nil {
			return err
		}
		attempts[t.ID] = as
	}

	ids := make([]string, 0, len(tareas))
	for _, t := range tareas {
		ids = append(ids, t.ID)
	}
	eventos := standup.Analizar(tareas, estadosPorID, attempts, loop.LeerLatidosCrudos(root, ids))
	if err := out.Data(eventos); err != nil {
		return err
	}
	if cfg, err := config.Load(root); err == nil {
		if cfg.PresupuestoTokens > 0 {
			out.Line("presupuesto %s", budget.Barra(budget.GastoEnDisco(root), cfg.PresupuestoTokens))
		}
		ventanasReg := ventanas.Nuevo(ventanas.LedgerPath(), cfg.PresupuestoVentanas)
		for _, p := range config.Clis {
			if l := ventanas.LineaVentanas(ventanasReg, p); l != "" {
				out.Line("presupuesto %s", l)
			}
		}
		if cfg.PresupuestoTokens > 0 || len(cfg.PresupuestoVentanas) > 0 {
			out.Line("")
		}
	}
	out.Line("%s", standup.Formatear(eventos, time.Now(), nActivas))

	for _, t := range tareas {
		nodos, err := recurse.LeerArbol(root, t.ID)
		if err != nil {
			return err
		}
		if len(nodos) == 0 {
			continue
		}
		out.Line("")
		out.Line("árbol de %s (%s):", t.ID, t.Titulo)
		imprimirArbolStandup(root, nodos, t.ID, 1)
	}
	return nil
}

// imprimirArbolStandup imprime, indentado, el estado de cada subtarea de
// una tarea recursiva — mismo dato que board, en el mismo lugar donde ya
// se mira el progreso del día. Una subtarea corriendo ahora mismo sale
// con ◐ y su fase, en vez de con el veredicto de cuando terminó.
func imprimirArbolStandup(root string, nodos []recurse.NodoArbol, padre string, profundidad int) {
	sangria := strings.Repeat("  ", profundidad)
	for _, n := range nodos {
		if n.Padre != padre {
			continue
		}
		marca := "✓"
		if !n.Verde {
			marca = "✗"
		}
		detalle := ""
		if n.Motivo != "" {
			detalle = " · " + n.Motivo
		}
		if n.Modelo != "" {
			detalle += " · " + n.Modelo
		}
		if l, corriendo := loop.LeerLatido(root, n.ID); corriendo {
			marca = "◐"
			detalle = " · " + l.Descripcion()
		}
		out.Line("%s%s %s  %s (%d intentos)%s", sangria, marca, n.ID, n.Titulo, n.Intentos, detalle)
		imprimirArbolStandup(root, nodos, n.ID, profundidad+1)
	}
}
