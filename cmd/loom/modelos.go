package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
)

func newModelosCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "modelos",
		Short: "elige el modelo de cada peso (liviana, media, pesada) del catálogo del CLI",
		Args:  cobra.NoArgs,
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
	cli, catalogo := detectarCatalogo(cfg.Cli)
	if len(catalogo) == 0 {
		return errors.New("el CLI no devolvió catálogo de modelos · revisa loom doctor")
	}
	if !esTUI() {
		for _, peso := range config.Pesos {
			out.Line("%s: %s", peso, cfg.Modelos[peso])
		}
		return errors.New("el panel necesita terminal · edita modelos: en .loom/config.yml")
	}
	actual := cfg.Modelos
	if cfg.Cli != cli || len(actual) == 0 {
		actual = config.ElegirModelos(catalogo)
	}
	elegidos, err := elegirPorPeso(actual, catalogo)
	if err != nil {
		return err
	}
	cfg.Cli, cfg.Modelos = cli, elegidos
	if err := cfg.Save(root); err != nil {
		return err
	}
	for _, peso := range config.Pesos {
		out.Line("✓ %s: %s", peso, elegidos[peso])
	}
	return nil
}
