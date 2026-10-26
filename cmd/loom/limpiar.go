package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func newLimpiarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "limpiar",
		Short: "libera los cuartos de las tareas que ya están en la rama base",
		Long: `Cada cuarto es una copia del repo con sus dependencias instaladas: en un
proyecto Node son más de 100 MB por tarea. limpiar quita el cuarto y la
rama de cada tarea lista cuyo trabajo ya está en la rama base, y los de
integración y entrega. No toca tareas pendientes, en curso o detenidas, ni
contratos, ni el registro de intentos.

loom archive lo hace solo con las tareas del feature que archiva.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			tareas, err := task.List(config.TasksDir(root))
			if err != nil {
				return err
			}
			ctx := context.Background()
			entregadas := ship.Entregadas(ctx, root, cfg.Base)
			var ids []string
			for _, t := range tareas {
				if st, err := state.Get(root, t.ID); err == nil && st.Estado == state.Lista && entregadas[t.ID] {
					ids = append(ids, t.ID)
				}
			}
			n := liberarCuartos(ctx, root, ids)
			if err := out.Data(map[string]int{"cuartos_liberados": n}); err != nil {
				return err
			}
			if n == 0 {
				out.Line("nada que limpiar · no hay cuartos de tareas ya entregadas")
				return nil
			}
			out.Line("✓ %d cuartos liberados", n)
			return nil
		},
	}
}

// liberarCuartos quita el cuarto y la rama de cada tarea de ids, y los
// cuartos de integración y de entrega cuando ya no queda ninguna tarea
// con cuarto. Devuelve cuántas carpetas quitó. La rama de entrega se
// queda: es el PR.
//
// En una corrida real, 12 cuartos con su node_modules eran 1.6 G de un .loom
// de 1.7 G, y `npx vitest run` en la raíz corría las pruebas de todos.
func liberarCuartos(ctx context.Context, root string, ids []string) int {
	dir := room.Dir(root)
	existe := func(id string) bool {
		_, err := os.Stat(filepath.Join(dir, id))
		return err == nil
	}
	n := 0
	for _, id := range ids {
		hay := existe(id)
		if room.Destroy(ctx, root, id) == nil && hay {
			n++
		}
	}
	// integración y entrega solo sirven mientras haya tareas en vuelo
	if es, _ := filepath.Glob(filepath.Join(dir, "T-*")); len(es) == 0 {
		if existe("_integra") && room.Destroy(ctx, root, "_integra") == nil {
			n++
		}
		if existe("_entrega") {
			room.Soltar(ctx, root, "_entrega")
			n++
		}
	}
	return n
}
