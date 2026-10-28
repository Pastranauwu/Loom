package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/wrap"
)

type wrapOpts struct {
	cli, curl, har, openapi, nombre string
	dry                             bool
}

func newWrapCmd() *cobra.Command {
	var o wrapOpts
	var modelo, ejecutor string
	cmd := &cobra.Command{
		Use:   "wrap --cli <binario> | --curl <comando|archivo> | --har <archivo> | --openapi <ruta|url>",
		Short: "convierte un CLI o una API en herramientas MCP (.loom/bridges)",
		Long: `Lee la ayuda de un binario, una llamada cURL, una captura HAR o un spec OpenAPI
y escribe un puente en .loom/bridges/<nombre>.json. loom mcp expone cada
herramienta del puente como bridge_<nombre>_<herramienta>.

CLI, cURL y HAR pasan por el planificador; OpenAPI se convierte por código.
Antes de guardar, la compuerta rechaza shells, variables que no estén en
required y binarios que no existan en el PATH.`,
		Example: `  loom wrap --cli jq
  loom wrap --curl 'curl -H "Authorization: Bearer x" https://api.example.com/v1/items'
  loom wrap --openapi ./openapi.yaml --name inventario
  loom wrap list`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var gen plan.Generador
			var root string
			var err error
			if o.openapi != "" {
				root, err = projectRoot()
			} else {
				var cfg config.Config
				root, cfg, err = entornoListo(false)
				if err == nil {
					if modelo == "" {
						modelo = config.ModeloRol(cfg, "planificador")
					}
					if ejecutor == "" {
						ejecutor = cfg.Cli
					}
					ex, errEx := elegirEjecutor(ejecutor)
					if errEx != nil {
						return errEx
					}
					gen = planGuardado{gen: generadorPlan{ex: ex, modelo: modelo, root: root}, root: root, archivo: "wrap-crudo.json"}
				}
			}
			if err != nil {
				return err
			}
			_, err = runWrap(root, gen, o, true)
			return err
		},
	}
	cmd.Flags().StringVar(&o.cli, "cli", "", "binario local a envolver")
	cmd.Flags().StringVar(&o.curl, "curl", "", "comando cURL o archivo que lo contiene")
	cmd.Flags().StringVar(&o.har, "har", "", "captura HAR del navegador")
	cmd.Flags().StringVar(&o.openapi, "openapi", "", "spec OpenAPI o Swagger (ruta o url)")
	cmd.Flags().StringVar(&o.nombre, "name", "", "nombre del puente (por defecto el del binario o del spec)")
	cmd.Flags().BoolVar(&o.dry, "dry-run", false, "muestra el puente sin guardarlo")
	cmd.Flags().StringVar(&modelo, "model", "", "modelo de la síntesis (por defecto el del planificador)")
	cmd.Flags().StringVar(&ejecutor, "executor", "", "CLI de la síntesis (por defecto el de config.yml)")
	cmd.AddCommand(newWrapListCmd(), newWrapShowCmd(), newWrapRmCmd())
	return cmd
}

// runWrap introspecciona la fuente, sintetiza el manifiesto, lo pasa por la
// compuerta y lo guarda. gen solo se usa para cli, curl y har.
func runWrap(root string, gen plan.Generador, o wrapOpts, smoke bool) (wrap.Manifest, error) {
	fuentes := 0
	for _, f := range []string{o.cli, o.curl, o.har, o.openapi} {
		if f != "" {
			fuentes++
		}
	}
	if fuentes != 1 {
		return wrap.Manifest{}, errors.New(idioma.T("pasa una sola fuente · --cli, --curl, --har o --openapi"))
	}
	var m wrap.Manifest
	var avisos []string
	if o.openapi != "" {
		var err error
		if m, avisos, err = wrap.FromOpenAPI(o.openapi, o.nombre); err != nil {
			return m, err
		}
	} else {
		modo, nombre, contexto := "api", o.nombre, ""
		switch {
		case o.cli != "":
			ayuda, err := wrap.HelpCLI(o.cli)
			if err != nil {
				return m, err
			}
			modo, contexto = "cli", ayuda
			if nombre == "" {
				nombre = o.cli
			}
		case o.curl != "":
			r, err := wrap.ParseCurl(o.curl)
			if err != nil {
				return m, err
			}
			contexto = wrap.Resumen(r)
		default:
			rs, err := wrap.ParseHAR(o.har)
			if err != nil {
				return m, err
			}
			contexto = wrap.Resumen(rs...)
		}
		if gen == nil {
			return m, errors.New(idioma.T("falta el modelo de la síntesis · revisa cli en .loom/config.yml"))
		}
		if err := esperarPlan(idioma.T("wrap · sintetizando el puente"), func(avance func(string)) error {
			r, err := conAvance(gen, avance).Generar(context.Background(), wrap.Prompt(modo, nombre, contexto))
			if err != nil {
				return err
			}
			m, err = wrap.Parse(r, nombre)
			return err
		}); err != nil {
			return m, err
		}
	}
	w, err := wrap.Verify(m, smoke)
	avisos = append(avisos, w...)
	for _, a := range avisos {
		out.Line("%s", a)
	}
	if err != nil {
		return m, fmt.Errorf(idioma.T("puente rechazado · %v"), err)
	}
	nombres := make([]string, len(m.Tools))
	for i, t := range m.Tools {
		nombres[i] = m.ToolName(t)
	}
	if o.dry {
		out.Line(idioma.T("puente %s · %d herramientas: %s"), m.Name, len(m.Tools), strings.Join(nombres, ", "))
		out.Line(idioma.T("\n--dry-run · no se guardó el puente"))
		return m, out.Data(m)
	}
	if err := wrap.Save(root, m); err != nil {
		return m, err
	}
	out.Line(idioma.T("✓ puente %s · %d herramientas: %s"), m.Name, len(m.Tools), strings.Join(nombres, ", "))
	return m, out.Data(m)
}

func newWrapListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "lista los puentes de .loom/bridges",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			ms, malos := wrap.List(root)
			for _, e := range malos {
				out.Line("· %v", e)
			}
			type fila struct {
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Tools       []string `json:"tools"`
			}
			filas := make([]fila, 0, len(ms))
			for _, m := range ms {
				f := fila{Name: m.Name, Description: m.Description}
				for _, t := range m.Tools {
					f.Tools = append(f.Tools, m.ToolName(t))
				}
				filas = append(filas, f)
				out.Line("%-16s %d  %s", m.Name, len(m.Tools), m.Description)
			}
			if len(ms) == 0 {
				out.Line(idioma.T("· sin puentes · loom wrap --cli <binario>"))
			}
			return out.Data(filas)
		},
	}
}

func newWrapShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <nombre>",
		Short: "muestra el manifiesto de un puente",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			m, err := wrap.Load(root, args[0])
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(m, "", "  ")
			out.Line("%s", b)
			return out.Data(m)
		},
	}
}

func newWrapRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <nombre>",
		Short: "borra un puente",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			if err := wrap.Remove(root, args[0]); err != nil {
				return err
			}
			out.Line(idioma.T("✓ puente %s borrado"), args[0])
			return out.Data(map[string]string{"removed": args[0]})
		},
	}
}
