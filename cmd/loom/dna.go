package main

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/constitution"
	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/tui"
)

// constitucionDe is the constitution plus the project's DNA block: the
// text every planning and implementation prompt carries as house style.
func constitucionDe(root string) (string, error) {
	c, err := constitution.Load(root)
	if err != nil {
		return "", err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return c, nil
	}
	b, desconocidos := dna.Bloque(config.Dir(root), cfg.DNA)
	if len(desconocidos) > 0 {
		out.Line(idioma.T("· dna desconocido en config.yml: %s · mira loom dna list"), strings.Join(desconocidos, ", "))
	}
	return strings.TrimSpace(c + "\n\n" + b), nil
}

func newDNACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dna [list|add|rm|show] [nombre]",
		Short: "elige el ADN de diseño que siguen los agentes (hexagonal, zero-deps, crash-early...)",
		Long: `El ADN son arquetipos de diseño que loom inyecta en cada prompt junto a la
constitución. Sin argumentos, en una terminal, abre un selector. Tus propios
arquetipos viven en .loom/dna/<nombre>.md y pisan a los incorporados.

Además del prompt, la esclusa de salida comprueba por código lo que se puede:
zero-deps bloquea dependencias nuevas en los manifiestos; hexagonal y
functional-core vedan que el dominio importe adaptadores o infraestructura.`,
		Example: `  loom dna
  loom dna add hexagonal
  loom dna rm zero-deps
  loom dna show`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runDNA(root, args)
		},
	}
	return cmd
}

func runDNA(root string, args []string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	dir := config.Dir(root)
	todos := dna.Lista(dir)
	if len(args) == 0 {
		if esTUI() {
			return elegirDNA(root, cfg, todos)
		}
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		activos := map[string]bool{}
		for _, n := range cfg.DNA {
			activos[n] = true
		}
		for _, a := range todos {
			marca := " "
			if activos[a.Nombre] {
				marca = "*"
			}
			origen := ""
			if a.Propio {
				origen = " · .loom/dna"
			}
			out.Line("%s %-16s %s%s", marca, a.Nombre, a.Resumen(), origen)
		}
		return out.Data(map[string]any{"activos": cfg.DNA, "disponibles": nombresDNA(todos)})
	case "show":
		b, _ := dna.Bloque(dir, cfg.DNA)
		if b == "" {
			out.Line(idioma.T("· sin dna · loom dna add <nombre>"))
			return nil
		}
		out.Line("%s", b)
		return nil
	case "add", "rm":
		if len(args) < 2 {
			return fmt.Errorf(idioma.T("falta el nombre · loom dna %s <nombre> · disponibles: %s"), args[0], strings.Join(nombresDNA(todos), ", "))
		}
		nombre := args[1]
		if args[0] == "add" {
			if !contieneDNA(nombresDNA(todos), nombre) {
				return fmt.Errorf(idioma.T("dna desconocido: %s · disponibles: %s"), nombre, strings.Join(nombresDNA(todos), ", "))
			}
			if !contieneDNA(cfg.DNA, nombre) {
				cfg.DNA = append(cfg.DNA, nombre)
			}
		} else {
			var resto []string
			for _, n := range cfg.DNA {
				if n != nombre {
					resto = append(resto, n)
				}
			}
			cfg.DNA = resto
		}
		if err := cfg.Save(root); err != nil {
			return err
		}
		out.Line(idioma.T("✓ dna activo: %s"), strings.Join(cfg.DNA, ", "))
		return nil
	}
	return fmt.Errorf(idioma.T("subcomando desconocido: %s · usa list, add, rm o show"), args[0])
}

func elegirDNA(root string, cfg config.Config, todos []dna.Arquetipo) error {
	activos := map[string]bool{}
	for _, n := range cfg.DNA {
		activos[n] = true
	}
	ops := make([]tui.Opcion, 0, len(todos))
	for _, a := range todos {
		ops = append(ops, tui.Opcion{ID: a.Nombre, Etiqueta: a.Nombre, Detalle: a.Resumen(), Marcada: activos[a.Nombre]})
	}
	ids, ok, err := tui.Marcar("ADN de diseño", "", ops)
	if err != nil || !ok {
		return err
	}
	cfg.DNA = ids
	if err := cfg.Save(root); err != nil {
		return err
	}
	out.Line(idioma.T("✓ dna activo: %s"), strings.Join(ids, ", "))
	return nil
}

func nombresDNA(as []dna.Arquetipo) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Nombre
	}
	return out
}

func contieneDNA(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
