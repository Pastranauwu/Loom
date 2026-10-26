package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
)

// elegirIdioma asks which language loom talks in. Prompts, spec keys and
// config stay in English whatever is chosen; this is the layer you read.
func elegirIdioma() (string, error) {
	ops := []tui.Opcion{
		{ID: "en", Etiqueta: "English", Detalle: "messages, help, map and tutorials in English"},
		{ID: "es", Etiqueta: "Español", Detalle: "mensajes, ayuda, mapa y tutorial en español"},
	}
	if l := os.Getenv("LANG"); len(l) >= 2 && l[:2] == "es" {
		ops[0], ops[1] = ops[1], ops[0]
	}
	return tui.Elegir("LANGUAGE / IDIOMA", "↑/↓ · enter", ops)
}

func newLanguageCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "language [en|es]",
		Aliases: []string{"idioma"},
		Short:   "set the language loom talks to you in",
		Long: `Sets the language of messages, help, the map and tutorials. Prompts, spec and
contract keys and config.yml stay in English: that is what the models get.
The choice is saved for you in ~/.loom/language and, inside a project, in its
config.yml.`,
		Example: "  loom language\n  loom language es\n  loom language en",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var l string
			switch {
			case len(args) == 1:
				l = args[0]
			case esTUI():
				var err error
				if l, err = elegirIdioma(); err != nil || l == "" {
					return err
				}
			default:
				out.Line("%s", idioma.Actual())
				return nil
			}
			if l != "en" && l != "es" {
				return fmt.Errorf("unknown language %q · use en or es", l)
			}
			if err := idioma.GuardarPreferido(l); err != nil {
				return err
			}
			if root, err := projectRoot(); err == nil {
				if cfg, err := config.Load(root); err == nil {
					cfg.Language = l
					_ = cfg.Save(root)
				}
			}
			idioma.Set(l)
			out.Line(idioma.T("✓ idioma: %s"), l)
			return nil
		},
	}
}
