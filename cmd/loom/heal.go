package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/heal"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func newHealCmd() *cobra.Command {
	var from, archivoSpec, modelo, ejecutor string
	var dry, entregar bool
	cmd := &cobra.Command{
		Use:   "heal [--from crash.log]",
		Short: "corrige el spec y el código a partir de un log de fallo",
		Long: `Lee un stacktrace o log de fallo (--from archivo, --from - o por pipe), averigua
qué regla o caso borde le faltaba al spec, la agrega al spec junto con un
criterio de aceptación cuyo comando reproduce el fallo, y corre la tarea
correctiva en su cuarto hasta que su listo_cuando pase. Con --ship la entrega.`,
		Example: `  loom heal --from crash.log
  go test ./... 2>&1 | loom heal
  loom heal --from crash.log --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, cfg, err := entornoListo(false)
			if err != nil {
				return err
			}
			log, err := leerLog(from, os.Stdin)
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
			t, err := curar(root, cfg, generadorPlan{ex: ex, modelo: modelo, root: root}, log, archivoSpec, dry)
			if err != nil || dry {
				return err
			}
			if err := runCmd(1, ejecutor, "", false, false); err != nil {
				return err
			}
			if st, err := state.Get(root, t.ID); err == nil && st.Estado == state.Lista {
				if entregar {
					return runShip(t.ID, false, false)
				}
				out.Line("✓ %s en verde · entrégala con loom ship %s", t.ID, t.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "archivo con el stacktrace o log (- o pipe = stdin)")
	cmd.Flags().StringVarP(&archivoSpec, "file", "f", "", "spec a corregir (por defecto el del repo)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "muestra el diagnóstico sin tocar el spec ni crear la tarea")
	cmd.Flags().BoolVar(&entregar, "ship", false, "entrega la tarea correctiva cuando quede verde")
	cmd.Flags().StringVar(&modelo, "model", "", "modelo del diagnóstico (por defecto el del planificador)")
	cmd.Flags().StringVar(&ejecutor, "executor", "", "CLI del diagnóstico (por defecto el de config.yml)")
	return cmd
}

// leerLog lee el log de --from, o de stdin con "-" o cuando llega por pipe.
func leerLog(from string, stdin *os.File) (string, error) {
	var r io.Reader
	switch {
	case from == "-" || (from == "" && !isTerminal(stdin)):
		r = stdin
	case from != "":
		f, err := os.Open(from)
		if err != nil {
			return "", fmt.Errorf("no pude leer %s · %w", from, err)
		}
		defer f.Close()
		r = f
	default:
		return "", errors.New("falta el log del fallo · loom heal --from crash.log (o por pipe)")
	}
	b, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", errors.New("el log está vacío · nada que diagnosticar")
	}
	return string(b), nil
}

// curar diagnostica el fallo, deja la regla y el criterio en el spec y crea
// la tarea correctiva. Con dry solo diagnostica.
func curar(root string, cfg config.Config, gen plan.Generador, log, archivoSpec string, dry bool) (task.Task, error) {
	ruta := archivoSpec
	if ruta == "" {
		var err error
		if ruta, err = spec.Find(root); err != nil {
			return task.Task{}, errors.New("heal parte de un spec · crea uno con loom sketch o loom.spec.yml")
		}
	} else if !filepath.IsAbs(ruta) {
		ruta = filepath.Join(root, ruta)
	}
	crudo, err := os.ReadFile(ruta)
	if err != nil {
		return task.Task{}, err
	}
	texto := string(crudo)
	if len(texto) > 8000 {
		texto = texto[:8000] + "\n# ..."
	}
	recorte := heal.Recortar(log)

	var d heal.Diagnostico
	if err := esperarPlan(idioma.T("heal · buscando qué le faltaba al spec"), func(avance func(string)) error {
		r, err := conAvance(gen, avance).Generar(context.Background(), heal.Prompt(recorte, texto, resumenGrafo(root, recorte), cfg.Pruebas))
		if err != nil {
			return err
		}
		d, err = heal.Parse(r)
		return err
	}); err != nil {
		return task.Task{}, err
	}

	out.Line("")
	out.Line(idioma.T("causa      %s"), d.Causa)
	out.Line(idioma.T("regla      %s"), d.Invariante)
	out.Line(idioma.T("criterio   %s"), d.Criterio)
	out.Line(idioma.T("reproduce  %s"), d.Comando)
	out.Line(idioma.T("tocar      %s"), strings.Join(append(append([]string{}, d.Tocar...), d.ArchivoPrueba), ", "))
	if dry {
		out.Line("\n--dry-run · no se tocó el spec ni se creó la tarea")
		return task.Task{}, nil
	}

	dir := config.TasksDir(root)
	id, err := task.NextID(dir)
	if err != nil {
		return task.Task{}, err
	}
	t := heal.Tarea(d, id, recorte)
	if errs := t.Validate(); len(errs) > 0 {
		return task.Task{}, fmt.Errorf("el diagnóstico produjo un contrato inválido: %v · vuelve a intentarlo", errs[0])
	}

	acc := spec.Acceptance{Criterion: d.Criterio, Command: d.Comando}
	if err := spec.AgregarReglaYCriterio(ruta, d.Invariante, acc); err != nil {
		out.Line("· el spec no se editó (%v) · agrega la regla a mano", err)
	} else {
		actualizarEstadoTrasHeal(root, ruta, acc)
		rel, _ := filepath.Rel(root, ruta)
		out.Line("✓ %s · regla y criterio agregados", rel)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return task.Task{}, err
	}
	if err := task.Save(dir, t); err != nil {
		return task.Task{}, err
	}
	out.Line("✓ %s · %s", t.ID, t.Titulo)
	return t, nil
}

// actualizarEstadoTrasHeal deja el estado aplicado al día con la regla y el
// criterio recién agregados: la tarea correctiva ya los cubre, y sin esto el
// siguiente apply los vería como cambio y replanearía.
func actualizarEstadoTrasHeal(root, ruta string, acc spec.Acceptance) {
	if f, err := spec.LoadFeatureState(root); err == nil {
		f.Acceptance = append(f.Acceptance, acc)
		_ = spec.SaveFeatureState(root, f)
	}
	s, err := spec.Load(ruta)
	if err != nil {
		return
	}
	clave, _ := filepath.Rel(root, ruta)
	clave = filepath.ToSlash(clave)
	if _, hay := spec.Previo(root, clave, s); hay {
		_ = spec.SaveEstadoSpec(root, clave, spec.Snapshot(s))
	}
}
