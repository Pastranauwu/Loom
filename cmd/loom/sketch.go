package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/sketch"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newSketchCmd() *cobra.Command {
	var destino, modelo, ejecutor string
	var forzar bool
	cmd := &cobra.Command{
		Use:   "sketch [\"idea\"]",
		Short: "te entrevista sobre tu idea y escribe el spec",
		Long: `Un modelo hace cuatro preguntas sobre los casos borde, reglas y fallos que tu
idea no menciona; tú respondes en una línea cada una y loom escribe
loom.spec.yml con lo que salió de la entrevista.`,
		Example: `  loom sketch "una cola de trabajos con reintentos"
  loom sketch -o specs/colas.yml "colas"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, cfg, err := entornoListo(false)
			if err != nil {
				return err
			}
			if modelo == "" {
				modelo = config.ModeloRol(cfg, "planificador")
			}
			if ejecutor == "" {
				ejecutor = cfg.Cli
			}
			ex, err := elegirEjecutor(ejecutor)
			if err != nil {
				return err
			}
			gen := generadorPlan{ex: ex, modelo: modelo, root: root}
			return runSketch(root, cfg, gen, os.Stdin, strings.TrimSpace(strings.Join(args, " ")), destino, forzar)
		},
	}
	cmd.Flags().StringVarP(&destino, "out", "o", "", "dónde escribir el spec (por defecto loom.spec.yml)")
	cmd.Flags().BoolVar(&forzar, "force", false, "sobrescribe un spec existente")
	cmd.Flags().StringVar(&modelo, "modelo", "", "modelo del entrevistador (por defecto el del planificador)")
	cmd.Flags().StringVar(&ejecutor, "ejecutor", "", "CLI del entrevistador (por defecto el de config.yml)")
	return cmd
}

// runSketch hace la entrevista y escribe el spec. En una terminal las
// preguntas van por la TUI; si no, por líneas de in.
func runSketch(root string, cfg config.Config, gen plan.Generador, in io.Reader, idea, destino string, forzar bool) error {
	tuiMode := esTUI() && isTerminal(os.Stdin)
	leer := bufio.NewReader(in)
	pregunta := func(titulo, ayuda string) (string, bool) {
		if tuiMode {
			v, ok, err := tui.Preguntar(titulo, ayuda, "")
			return v, ok && err == nil
		}
		out.Line("%s", titulo)
		linea, err := leer.ReadString('\n')
		if err != nil && linea == "" {
			return "", false
		}
		return strings.TrimSpace(linea), true
	}

	ruta, err := destinoSketch(root, destino, forzar)
	if err != nil {
		return err
	}
	if idea == "" {
		var ok bool
		if idea, ok = pregunta("¿Qué quieres construir?", "una línea · enter envía · esc cancela"); !ok || idea == "" {
			return errors.New("sin idea no hay spec · loom sketch \"tu idea\"")
		}
	}
	repo := contextoRepoSketch(root, cfg)

	var preguntas []sketch.Pregunta
	if err := esperarPlan("sketch · buscando lo que no pensaste", func(avance func(string)) error {
		texto, err := conAvance(gen, avance).Generar(context.Background(), sketch.PromptPreguntas(idea, repo))
		if err != nil {
			return err
		}
		preguntas, err = sketch.ParsePreguntas(texto)
		return err
	}); err != nil {
		return err
	}

	var qa []sketch.Respuesta
	for i, q := range preguntas {
		titulo := fmt.Sprintf("%d/%d · %s", i+1, len(preguntas), q.Pregunta)
		ayuda := "enter sin escribir = que decida loom · esc cancela"
		if q.Porque != "" {
			ayuda = "si se responde mal: " + q.Porque + " · " + ayuda
		}
		r, ok := pregunta(titulo, ayuda)
		if !ok {
			out.Line("cancelado · no se escribió nada")
			return nil
		}
		qa = append(qa, sketch.Respuesta{Pregunta: q.Pregunta, Respuesta: r})
	}

	var s spec.Spec
	if err := esperarPlan("sketch · armando el spec", func(avance func(string)) error {
		texto, err := conAvance(gen, avance).Generar(context.Background(), sketch.PromptSpec(idea, qa, repo))
		if err != nil {
			return err
		}
		s, err = sketch.ParseSpec(texto)
		return err
	}); err != nil {
		return err
	}

	contenido := spec.Marshal(s)
	out.Line("")
	out.Line("%s", strings.TrimRight(string(contenido), "\n"))
	out.Line("")
	if !confirmarSketch(tuiMode, leer, ruta) {
		out.Line("descartado · no se escribió nada")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ruta), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(ruta, contenido, 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, ruta)
	out.Line("✓ spec escrito en %s · siguiente: loom apply --dry-run · loom up", rel)
	return out.Data(map[string]any{"spec": rel, "feature": s.Feature, "requirements": len(s.Requirements)})
}

// destinoSketch resuelve dónde se escribe y rechaza pisar un spec existente.
func destinoSketch(root, destino string, forzar bool) (string, error) {
	if destino != "" {
		if !filepath.IsAbs(destino) {
			destino = filepath.Join(root, destino)
		}
		if _, err := os.Stat(destino); err == nil && !forzar {
			return "", fmt.Errorf("%s ya existe · usa --force o -o otra/ruta.yml", destino)
		}
		return destino, nil
	}
	if existente, err := spec.Find(root); err == nil && !forzar {
		return "", fmt.Errorf("ya hay un spec en %s · usa --force o -o otra/ruta.yml", existente)
	}
	return filepath.Join(root, "loom.spec.yml"), nil
}

func confirmarSketch(tuiMode bool, leer *bufio.Reader, ruta string) bool {
	if tuiMode {
		id, err := tui.Elegir("¿Escribir "+filepath.Base(ruta)+"?", "", []tui.Opcion{
			{ID: "si", Etiqueta: "escribir el spec"}, {ID: "no", Etiqueta: "descartar"},
		})
		return err == nil && id == "si"
	}
	out.Line("¿escribir %s? [s/n]", ruta)
	linea, _ := leer.ReadString('\n')
	r := strings.ToLower(strings.TrimSpace(linea))
	return r == "s" || r == "si" || r == "y" || r == "yes"
}

// conAvance engancha los avances del spinner al generador si lo admite.
func conAvance(g plan.Generador, avance func(string)) plan.Generador {
	if gp, ok := g.(generadorPlan); ok {
		gp.avance = avance
		return gp
	}
	return g
}

func contextoRepoSketch(root string, cfg config.Config) string {
	if config.DetectEmpty(root) {
		return ""
	}
	s := "Language: " + config.DetectLanguage(root) + "."
	if cfg.Pruebas != "" {
		s += " Test command: " + cfg.Pruebas + "."
	}
	return s + "\nTracked files:\n" + arbolArchivosConstitution(root)
}
