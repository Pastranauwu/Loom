package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/constitution"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newConstitutionCmd() *cobra.Command {
	var modelo, ejecutorFlag string
	var forzar bool
	cmd := &cobra.Command{
		Use:   "constitution",
		Short: "genera la constitución del proyecto",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConstitution(modelo, ejecutorFlag, forzar)
		},
	}
	cmd.Flags().StringVar(&modelo, "modelo", "", "modelo del planificador")
	cmd.Flags().StringVar(&ejecutorFlag, "ejecutor", "", "opencode o claude")
	cmd.Flags().BoolVarP(&forzar, "forzar", "f", false, "regenerar si ya existe")
	return cmd
}

func runConstitution(modelo, ejecutorFlag string, forzar bool) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	if constitution.Exists(root) && !forzar {
		out.Line("constitución existente en %s · usa --forzar para regenerarla", constitution.Path(root))
		return nil
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if modelo == "" {
		modelo = config.ModeloRol(cfg, "planificador")
	}
	if ejecutorFlag == "" {
		ejecutorFlag = cfg.Cli
	}
	ex, err := elegirEjecutor(ejecutorFlag)
	if err != nil {
		return err
	}

	arbol := arbolArchivosConstitution(root)
	prompt := constitution.Prompt(config.DetectLanguage(root), cfg.Pruebas, arbol)

	var contenido string
	generar := func() error {
		res, err := ex.Run(context.Background(), executor.Request{
			Rol:      executor.RolTexto,
			RoomPath: root,
			Prompt:   prompt,
			Model:    modelo,
			Timeout:  3 * time.Minute,
		})
		if err != nil {
			return err
		}
		contenido = res.Text
		return nil
	}

	if esTUI() {
		err = tui.Esperar("generando constitución · "+modelo, generar)
	} else {
		err = generar()
	}
	if err != nil {
		return err
	}

	out.Line("%s", contenido)
	if isTerminal(os.Stdin) {
		out.Line("\n¿guardar en %s? [s/n]", constitution.Path(root))
		linea, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		r := strings.ToLower(strings.TrimSpace(linea))
		if r != "s" && r != "si" && r != "y" && r != "yes" {
			out.Line("descartada")
			return nil
		}
	}
	if err := constitution.Save(root, contenido); err != nil {
		return err
	}
	out.Line("✓ constitución guardada en %s · versiona este archivo en git", constitution.Path(root))
	return nil
}

// arbolArchivosConstitution lists tracked files for model context.
func arbolArchivosConstitution(root string) string {
	cmd := exec.Command("git", "-C", root, "ls-files", "--directory")
	b, _ := cmd.Output()
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 50 {
		lines = append(lines[:50], "... (truncado)")
	}
	return strings.Join(lines, "\n")
}
