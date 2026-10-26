package main

import (
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

func runMap(root string, mermaid, todos bool, profundidad int) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	e := topo.Entrada{Profundidad: profundidad, Todos: todos, Estados: map[string]string{}}
	if g, _, err := graph.Update(root, config.Dir(root)); err == nil {
		e.Grafo = g
	} else {
		out.Line("· sin grafo del repo (%v) · el mapa sale solo de los contratos", err)
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

	m := topo.Construir(e)
	if out.JSON() {
		return out.Data(m)
	}
	if mermaid {
		out.Line("%s", topo.Mermaid(m))
		return nil
	}
	estilo := topo.Estilo{}
	if esTUI() {
		estilo = topo.Estilo{Titulo: tui.Titulo, Apagado: tui.Apagado, Ok: tui.Presion, Mal: tui.Alerta, Espera: tui.Espera}
	}
	out.Line("%s", strings.TrimRight(topo.Texto(m, estilo), "\n"))
	return nil
}
