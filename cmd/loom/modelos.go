package main

import (
	"errors"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
)

func newModelosCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "models",
		Aliases: []string{"modelos"},
		Short:   "elige el modelo de cada peso (liviana, media, pesada) del catálogo del CLI",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			return runModelos(cwd)
		},
	}
}

func runModelos(cwd string) error {
	root, err := config.RepoRoot(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	cli, _ := detectarCatalogo(cfg.Cli)
	catalogo := catalogoTotal(cli)
	if len(catalogo) == 0 {
		return errors.New(idioma.T("el CLI no devolvió catálogo de modelos · revisa loom doctor"))
	}
	if !esTUI() {
		for _, peso := range config.Pesos {
			out.Line("%s: %s", peso, cfg.Modelos[peso])
		}
		out.Line(idioma.T("disponibles (de todos los CLIs instalados):"))
		for _, m := range catalogo {
			out.Line("  %s", m)
		}
		return errors.New(idioma.T("el panel necesita terminal · edita modelos: en .loom/config.yml"))
	}
	actual := cfg.Modelos
	if len(actual) == 0 {
		actual = config.ElegirModelos(catalogo)
	}
	elegidos, err := elegirPorPeso(actual, catalogo)
	if err != nil {
		return err
	}
	if cfg.Cli == "" {
		cfg.Cli = cli
	}
	cfg.Modelos = elegidos
	if err := cfg.Save(root); err != nil {
		return err
	}
	for _, peso := range config.Pesos {
		out.Line("✓ %s: %s", peso, elegidos[peso])
	}
	return nil
}

// catalogoTotal junta el catálogo de cada CLI instalado, el de primero
// adelante: modelos: mezcla CLIs (ejecutorPara), así que una key de
// DeepSeek en opencode vale aunque cli: sea claude.
func catalogoTotal(primero string) []string {
	clis := []string{primero}
	for _, c := range clisInstalados() {
		if c != primero {
			clis = append(clis, c)
		}
	}
	var todo []string
	for _, c := range clis {
		if ex, err := elegirEjecutor(c); err == nil {
			cat, _ := catalogoDe(ex)
			todo = append(todo, cat...)
		}
	}
	return todo
}
