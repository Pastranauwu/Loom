package main

import (
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/graph"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/topo"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newMapCmd() *cobra.Command {
	var mermaid, todos bool
	var profundidad int
	cmd := &cobra.Command{
		Use:     "map",
		Aliases: []string{"view"},
		Short:   "mapa de módulos, contratos y compuertas sin abrir código",
		Long: `Dibuja el sistema a alto nivel con lo que loom ya sabe: los módulos, los
contratos públicos (expone) que ofrece cada uno, quién consume a quién (usa e
imports), el ADN activo y el estado de las compuertas. No abre archivos de
código: sale del grafo del repo y de los contratos de tarea.

En terminal es un árbol; --mermaid exporta un diagrama para Markdown o un PR;
--json, la estructura completa.`,
		Example: `  loom map
  loom map --mermaid > docs/arquitectura.md
  loom map --depth 3 --all
  loom view --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runMap(root, mermaid, todos, profundidad)
		},
	}
	cmd.Flags().BoolVar(&mermaid, "mermaid", false, "diagrama Mermaid en vez del árbol")
	cmd.Flags().BoolVar(&todos, "all", false, "incluye los módulos sin tareas ni contratos")
	cmd.Flags().IntVar(&profundidad, "depth", 2, "segmentos de ruta que nombran un módulo")
	return cmd
}

// construirMapa reads the repo and the contracts into the map. A graph
// failure is returned apart: the map still comes out, from the contracts.
func construirMapa(root string, todos bool, profundidad int) (topo.Mapa, error, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return topo.Mapa{}, nil, err
	}
	e := topo.Entrada{Profundidad: profundidad, Todos: todos, Estados: map[string]string{}}
	var grafoErr error
	if g, _, err := graph.Update(root, config.Dir(root)); err == nil {
		e.Grafo = g
	} else {
		grafoErr = err
	}
	if tareas, err := task.List(config.TasksDir(root)); err == nil {
		e.Tareas = tareas
		for _, t := range tareas {
			if st, err := state.Get(root, t.ID); err == nil {
				e.Estados[t.ID] = st.Estado
			}
		}
	}
	activos := map[string]bool{}
	for _, n := range cfg.DNA {
		activos[n] = true
	}
	for _, a := range dna.Lista(config.Dir(root)) {
		if activos[a.Nombre] {
			e.DNA = append(e.DNA, a)
		}
	}
	if a, hay := ship.LeerAceptacion(root); hay {
		e.Aceptacion = "verde"
		if !a.Aprobado {
			e.Aceptacion, e.Motivo = "roja", a.Motivo
		}
	}

	return topo.Construir(e), grafoErr, nil
}

func runMap(root string, mermaid, todos bool, profundidad int) error {
	m, grafoErr, err := construirMapa(root, todos, profundidad)
	if err != nil {
		return err
	}
	if grafoErr != nil {
		out.Line(idioma.T("· sin grafo del repo (%v) · el mapa sale solo de los contratos"), grafoErr)
	}
	if out.JSON() {
		return out.Data(m)
	}
	if mermaid {
		out.Line("%s", topo.Mermaid(m))
		return nil
	}
	if !esTUI() {
		out.Line("%s", strings.TrimRight(topo.Texto(m, topo.Estilo{}), "\n"))
		return nil
	}
	ancho, alto := tui.Terminal(os.Stdout.Fd())
	vista := tui.VistaMapa(m, ancho)
	if strings.Count(vista, "\n") >= alto-1 {
		return tui.Visor(vista)
	}
	out.Line("%s", vista)
	return nil
}
