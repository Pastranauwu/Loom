package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newTemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "theme [nombre]",
		Aliases: []string{"tema"},
		Short:   "elige el tema de colores de la TUI y de loom ide",
		Long: `Sin nombre y en una terminal, muestra los temas para elegir; sin terminal, los
lista. El tema se guarda para ti en ~/.loom/theme y vale en todos tus
proyectos. Si loom ide está abierto, lo repinta en el momento.`,
		Example: "  loom theme\n  loom theme nord",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var nombre string
			switch {
			case len(args) == 1:
				nombre = args[0]
			case esTUI():
				var ops []tui.Opcion
				for _, t := range tui.Temas {
					ops = append(ops, tui.Opcion{ID: t.Nombre, Etiqueta: t.Nombre, Detalle: tui.Muestra(t)})
				}
				var err error
				if nombre, err = tui.Elegir(strings.ToUpper(idioma.T("tema")), "↑/↓ · enter", ops); err != nil || nombre == "" {
					return err
				}
			default:
				for _, t := range tui.Temas {
					marca := " "
					if t.Nombre == tui.TemaActual().Nombre {
						marca = "*"
					}
					out.Line("%s %-12s %s", marca, t.Nombre, tui.Muestra(t))
				}
				return nil
			}
			if _, ok := tui.BuscarTema(nombre); !ok {
				var nombres []string
				for _, t := range tui.Temas {
					nombres = append(nombres, t.Nombre)
				}
				return fmt.Errorf(idioma.T("tema desconocido %q · usa uno de: %s"), nombre, strings.Join(nombres, ", "))
			}
			if err := tui.GuardarTema(nombre); err != nil {
				return err
			}
			tui.AplicarTema(nombre)
			out.Line(idioma.T("✓ tema %s"), nombre)
			repintarIDE()
			return nil
		},
	}
	cmd.AddCommand(newTemaAddCmd())
	return cmd
}

func newTemaAddCmd() *cobra.Command {
	var base string
	var usar bool
	cmd := &cobra.Command{
		Use:   "add <nombre> [color=#RRGGBB]...",
		Short: "guarda un tema propio a partir de otro",
		Long: `Crea un tema en ~/.loom/themes/<nombre>.json a partir de --from (loom por
defecto) con los colores que cambies: background, surface, border, muted,
text, accent, alert, warning. Con --use queda puesto.`,
		Example: "  loom theme add dracula --from tokyo-night accent=#bd93f9 alert=#ff5555 --use",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cambios := map[string]string{}
			for _, a := range args[1:] {
				k, v, ok := strings.Cut(a, "=")
				if !ok {
					return fmt.Errorf(idioma.T("%q no es color=#RRGGBB"), a)
				}
				cambios[strings.ToLower(k)] = v
			}
			t, err := tui.NuevoTema(args[0], base, cambios)
			if err != nil {
				return err
			}
			if err := tui.GuardarTemaPropio(t); err != nil {
				return err
			}
			out.Line(idioma.T("✓ tema %s guardado · %s"), t.Nombre, tui.Muestra(t))
			if usar {
				if err := tui.GuardarTema(t.Nombre); err != nil {
					return err
				}
				tui.AplicarTema(t.Nombre)
				repintarIDE()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "from", "loom", "tema del que parte")
	cmd.Flags().BoolVar(&usar, "use", false, "lo deja puesto")
	return cmd
}

// repintarIDE aplica el tema a la sesión de loom ide de este proyecto, si
// está abierta: estilo de tmux y paneles relanzados para que lo tomen.
func repintarIDE() {
	root, err := projectRoot()
	if err != nil {
		return
	}
	s := sesionIDE(root)
	if exec.Command("tmux", "has-session", "-t", s).Run() != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	estiloIDE(s, root, exe)
	for _, id := range leerPaneles(s).paneles {
		_ = tmux("respawn-pane", "-k", "-t", id)
	}
}
