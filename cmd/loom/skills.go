package main

import (
	"context"
	"github.com/Pastranauwu/loom/internal/idioma"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/skills"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "gestiona las skills reales inyectadas en el prompt de cada agente",
	}
	cmd.AddCommand(newSkillsSyncCmd())
	return cmd
}

func newSkillsSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "trae con npx los paquetes de skill que usan los agentes del catálogo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsSync()
		},
	}
	return cmd
}

// runSkillsSync trae cada paquete de skill que algún agente (por defecto o
// personalizado en config.yml) referencia en SkillPackages. Best-effort:
// una skill que no se pudo traer se reporta y no bloquea a las demás — el
// agente sigue funcionando sin ella (loop.go degrada solo).
func runSkillsSync() error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	nombres := skillPackageNamesEnUso(cfg)
	fuentes := map[string]skills.Source{}
	for _, s := range skills.DefaultSources() {
		fuentes[s.Nombre] = s
	}

	ok, fallidas := 0, 0
	for _, nombre := range nombres {
		src, conocida := fuentes[nombre]
		if !conocida {
			out.Line(idioma.T("⚠ %s · sin fuente conocida (agrégala en config.yml con su repo o pide a loom que la reconozca)"), nombre)
			continue
		}
		if skills.Instalado(root, nombre) {
			out.Line(idioma.T("· %s · ya instalada"), nombre)
			ok++
			continue
		}
		if err := skills.Fetch(context.Background(), root, src); err != nil {
			out.Line("✗ %s · %s", nombre, err)
			fallidas++
			continue
		}
		out.Line("✓ %s", nombre)
		ok++
	}
	out.Line(idioma.T("%d skills listas, %d fallidas"), ok, fallidas)
	return nil
}

// skillPackageNamesEnUso junta, sin duplicados, todos los SkillPackages
// del catálogo por defecto y de los agentes personalizados en config.yml.
func skillPackageNamesEnUso(cfg config.Config) []string {
	vistos := map[string]bool{}
	var nombres []string
	agregar := func(ns []string) {
		for _, n := range ns {
			if !vistos[n] {
				vistos[n] = true
				nombres = append(nombres, n)
			}
		}
	}
	for _, ag := range config.DefaultAgentes(cfg.Cli) {
		agregar(ag.SkillPackages)
	}
	for _, ag := range cfg.Agentes {
		agregar(ag.SkillPackages)
	}
	return nombres
}
