package main

import (
	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/graph"
)

func newGraphCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "graph",
		Short: "indexa el repo en .loom/repo-graph.json (incremental)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			g, n, err := graph.Update(root, config.Dir(root))
			if err != nil {
				return err
			}
			out.Line("grafo · %d archivos · %d actualizados", len(g.Files), n)
			out.Data(map[string]any{"archivos": len(g.Files), "actualizados": n, "commit_sha": g.CommitSHA})
			return nil
		},
	}
}

// resumenGrafo actualiza el grafo y devuelve su resumen; degrada en abierto.
func resumenGrafo(root, consulta string) string {
	g, _, err := graph.Update(root, config.Dir(root))
	if err != nil {
		out.Line("· grafo del repo no disponible (%v) · el planificador explora por su cuenta", err)
		return ""
	}
	return graph.Resumen(g, consulta)
}
