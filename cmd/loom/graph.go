package main

import (
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/graph"
	"github.com/Pastranauwu/loom/internal/topo"
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
			out.Line(idioma.T("grafo · %d archivos · %d actualizados"), len(g.Files), n)
			out.Data(map[string]any{"archivos": len(g.Files), "actualizados": n, "commit_sha": g.CommitSHA})
			return nil
		},
	}
}

// resumenGrafo actualiza el grafo y devuelve su resumen; degrada en abierto.
func resumenGrafo(root, consulta string) string {
	g, _, err := graph.Update(root, config.Dir(root))
	if err != nil {
		out.Line(idioma.T("· grafo del repo no disponible (%v) · el planificador explora por su cuenta"), err)
		return ""
	}
	return graph.Resumen(g, consulta) + detallesRelevantes(g, consulta)
}

const (
	maxModulosDetalle = 2
	maxBytesDetalle   = 3000
)

// detallesRelevantes es el `loom map <módulo>` de los módulos donde están
// los archivos que más coinciden con el pedido: qué hace cada archivo,
// quién lo usa y qué pruebas lo cubren. Con eso el planificador sabe qué
// abrir y qué pruebas correr sin explorar el repo turno por turno (cada
// turno relee su contexto). Vacío si el pedido no coincide con nada.
func detallesRelevantes(g *graph.Graph, consulta string) string {
	e := topo.Entrada{Grafo: g, Profundidad: 2}
	var b strings.Builder
	vistos := map[string]bool{}
	for _, f := range graph.Relevantes(g, consulta) {
		if len(vistos) == maxModulosDetalle {
			break
		}
		d, err := topo.Detalle(e, f)
		if err != nil {
			continue
		}
		cab, _, _ := strings.Cut(d, " ·")
		if vistos[cab] {
			continue
		}
		vistos[cab] = true
		if len(d) > maxBytesDetalle {
			d = d[:strings.LastIndex(d[:maxBytesDetalle], "\n")] + "\n  …"
		}
		b.WriteString("\n\nMódulo " + d)
	}
	return b.String()
}
