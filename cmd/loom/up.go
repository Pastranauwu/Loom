package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/spec"
)

func newUpCmd() *cobra.Command {
	var file string
	var agentes int
	var modelo, ejecutor, titulo string
	var reintentar, entregar, integrar, revisar, fondo, local bool

	cmd := &cobra.Command{
		Use:   `up ["<petición>"]`,
		Short: "de una petición a un PR limpio, sin pasos intermedios",
		Long: `Encadena todo el trabajo de loom en un solo comando.

Con una petición en lenguaje natural, la parte en tareas (loom plan),
las ejecuta en paralelo en cuartos aislados (loom run) y, con --ship,
las entrega en UN pull request limpio (loom ship --todas).

Sin petición, aplica la especificación del repo (loom.spec.yml) si
existe y ejecuta las tareas pendientes.

No hace falta loom init: up configura lo que falte (repo git,
.loom, commit inicial, ejecutor, modelos, comando de pruebas) y
pregunta solo cuando no puede resolverlo solo.`,
		Example: `  loom up "cli en go que despierta equipos por wake-on-lan" --ship
  loom up "arreglar el login con tildes" --integrar
  loom up --agentes 4 --ship
  loom up -f specs/auth.yml`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _, err := entornoListoConCLI((entregar || integrar || revisar) && !local, ejecutor)
			if err != nil {
				return err
			}
			// en up el desprendimiento cubre el encadenado completo
			// (plan, run y la entrega), no solo la corrida: lo que dura
			// es el conjunto. Va después de preparar el entorno, que es
			// lo único que puede preguntar.
			if fondo {
				return lanzarEnFondo(root)
			}
			defer marcarCorrida(root)()

			var s spec.Spec
			if frase := strings.TrimSpace(strings.Join(args, " ")); frase != "" {
				// una petición manda sobre la spec: es lo que el humano
				// acaba de pedir, aquí y ahora. Va por el mismo esqueleto
				// que un spec con requirements
				var err error
				s, err = aplicarSpec(root, spec.Spec{Feature: frase, Requirements: []string{frase}}, "la petición", false, false)
				if err != nil {
					return err
				}
				out.Line("")
				if titulo == "" {
					titulo = frase
				}
			} else {
				specFile := file
				if specFile == "" {
					if found, err := spec.Find(root); err == nil {
						specFile = found
					}
				}
				if specFile != "" {
					var err error
					s, err = runApply(root, specFile, false, false)
					if err != nil {
						return err
					}
					out.Line("")
				}
			}

			// el spec fija agentes y entrega cuando la línea de comandos
			// no los pide: --agentes gana sobre `agentes:`, y `ship: true`
			// entrega aunque no se haya pasado --ship
			if !cmd.Flags().Changed("agentes") && s.Agentes > 0 {
				agentes = s.Agentes
			}
			if s.Ship {
				entregar = true
			}
			if titulo == "" {
				titulo = s.Feature
			}
			if err := runCmd(agentes, ejecutor, modelo, reintentar, false); err != nil {
				return err
			}
			if !entregar && !integrar && !revisar {
				return nil
			}
			out.Line("")
			return runShipTodas(false, titulo, integrar, revisar, local)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "ruta al archivo de especificación (.yml)")
	cmd.Flags().IntVar(&agentes, "agentes", 0, "número de trabajadores en paralelo (0 = automático, hasta 16 y 3 con modelo de pago)")
	cmd.Flags().StringVar(&modelo, "modelo", "", "fuerza un modelo para todas las tareas")
	cmd.Flags().StringVar(&ejecutor, "ejecutor", "", "opencode o claude")
	cmd.Flags().BoolVar(&reintentar, "reintentar", false, "vuelve a correr también las tareas detenidas")
	cmd.Flags().BoolVar(&entregar, "ship", false, "al terminar, entrega todas las tareas listas en un solo PR")
	cmd.Flags().BoolVar(&revisar, "revisar", false, "además de entregar, un modelo revisa el diff y deja el informe en el PR")
	cmd.Flags().BoolVar(&integrar, "integrar", false, "además de revisar, mergea el PR si el revisor no pide cambios")
	cmd.Flags().BoolVar(&local, "local", false, "la entrega queda como PR local (rama y descripción en el repo) aunque haya remoto")
	cmd.Flags().StringVar(&titulo, "titulo", "", "título del PR (por defecto, la petición)")
	cmd.Flags().BoolVar(&fondo, "fondo", false, "desprende todo el encadenado de la terminal y devuelve el control; sigue corriendo si la cierras")

	return cmd
}
