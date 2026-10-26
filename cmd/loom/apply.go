package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
)

func newApplyCmd() *cobra.Command {
	var file string
	var runImmediately bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "apply [-f archivo.spec.yml]",
		Short: "aplica una especificación declarativa de tareas (Requerimientos como Código)",
		Long: `Lee una especificación declarativa (loom.spec.yml por defecto), valida
los contratos de las tareas, asigna IDs correlativos a las que no tengan,
y las guarda en .loom/tasks/ listas para ser verificadas o ejecutadas.

Con --run (o -r), lanza inmediatamente la ejecución en paralelo (loom run).`,
		Example: `  loom apply
  loom apply -f specs/auth.yml
  loom apply --dry-run
  loom apply --run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			_, err = runApplyTodos(root, file, runImmediately, dryRun)
			return err
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "ruta al archivo de especificación (.yml)")
	cmd.Flags().BoolVarP(&runImmediately, "run", "r", false, "ejecuta las tareas inmediatamente tras aplicarlas")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "valida la especificación sin escribir archivos en disco")

	return cmd
}

// runApply aplica la especificación y la devuelve: quien llama (up) la
// necesita para respetar sus `agentes` y `ship`.
func runApply(root, filePath string, runImmediately, dryRun bool) (spec.Spec, error) {
	var err error
	if filePath == "" {
		filePath, err = spec.Find(root)
		if err != nil {
			return spec.Spec{}, fmt.Errorf("no se encontró archivo de especificación en %s · especifícalo con -f o crea loom.spec.yml", root)
		}
	} else if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(root, filePath)
	}

	s, err := spec.Load(filePath)
	if err != nil {
		return spec.Spec{}, fmt.Errorf("error al leer %s: %w", filePath, err)
	}
	// solo el modo requirements: ahí el plan entero lo genera el
	// arquitecto, y replanear un spec igual es pagarlo dos veces
	porRequirements := len(s.Tasks) == 0 && len(s.Requirements) > 0
	clave, _ := filepath.Rel(root, filePath)
	clave = filepath.ToSlash(clave)
	actual := spec.Snapshot(s)
	prev, ok := spec.Previo(root, clave, s)
	d := spec.Diff(prev, actual)
	if dryRun && !d.Vacio() {
		out.Line("%s", d)
	}
	if ok && d.Vacio() {
		if !dryRun {
			_ = spec.SaveEstadoSpec(root, clave, actual) // repo que venía de intencion.json o feature.json
		}
		out.Line("· %s sin cambios desde el último plan · sigo con las tareas pendientes (borra .loom/%s para replanear)", filepath.Base(filePath), spec.EstadoFile)
		return s, nil
	}
	if ok && porRequirements {
		s.Previos, s.PreviosIDs = prev.Requirements()
		s.Delta = d
	}
	viejo, _ := spec.LoadFeatureState(root)
	s, err = aplicarSpec(root, s, filepath.Base(filePath), runImmediately, dryRun)
	if err == nil && !dryRun {
		if len(spec.FindAll(root)) > 1 {
			err = spec.FusionarAceptacion(root, viejo, prev, s)
		}
		if err == nil {
			err = spec.SaveEstadoSpec(root, clave, actual)
		}
	}
	return s, err
}

// runApplyTodos aplica el spec de -f o, sin él, todos los specs vivos
// (spec.FindAll) en orden. Devuelve el primero: de él salen `agentes` y `ship`.
func runApplyTodos(root, filePath string, runImmediately, dryRun bool) (spec.Spec, error) {
	archivos := []string{filePath}
	if filePath == "" {
		if archivos = spec.FindAll(root); len(archivos) == 0 {
			archivos = []string{""}
		}
	}
	var primero spec.Spec
	for i, f := range archivos {
		s, err := runApply(root, f, runImmediately, dryRun)
		if err != nil {
			return spec.Spec{}, err
		}
		if i == 0 {
			primero = s
		}
	}
	return primero, nil
}

// aplicarSpec completa, valida y escribe los contratos de un spec ya
// cargado. origen es el nombre con que se lo muestra al humano.
func aplicarSpec(root string, s spec.Spec, origen string, runImmediately, dryRun bool) (spec.Spec, error) {
	filePath := origen
	if n := contarSinContrato(s.Tasks); n > 0 || (len(s.Tasks) == 0 && len(s.Requirements) > 0) {
		if dryRun {
			// en seco no se gastan tokens: se marca lo que completará la IA
			out.Line("· %d tareas sin contrato completo · el planificador las completa al aplicar", n)
			for i := range s.Tasks {
				if strings.TrimSpace(s.Tasks[i].ListoCuando) == "" {
					s.Tasks[i].ListoCuando = "(lo completa el planificador)"
				}
			}
		} else if err := completarSpec(root, &s); err != nil {
			return spec.Spec{}, fmt.Errorf("no se pudo completar %s: %w", filepath.Base(filePath), err)
		}
	}

	tasksDir := config.TasksDir(root)
	applied, err := spec.Apply(tasksDir, s, dryRun)
	if err != nil {
		return spec.Spec{}, err
	}
	// sin tareas nuevas, "ninguna tarea cubre X" es cierto de todo el
	// spec y no dice nada
	for _, issue := range spec.ValidatePlan(s, applied) {
		if issue.Level == "warning" && len(applied) > 0 {
			out.Line("· plan: %s", issue.Message)
		}
	}
	// la aceptación que nombra una prueba inexistente frena la entrega al
	// final, con todo pagado: se dice ahora
	enRepo, _ := task.List(tasksDir)
	for _, issue := range spec.AceptacionSinPrueba(root, s, append(enRepo, applied...)) {
		out.Line("· plan: %s", issue.Message)
	}

	if dryRun {
		out.Line("✓ especificación válida (%d tareas en %s, modo --dry-run):", len(applied), filepath.Base(filePath))
		for _, t := range applied {
			ag := ""
			if t.Agente != "" {
				ag = " [" + t.Agente + "]"
			}
			out.Line("  %s  %s%s  · listo cuando: %s", t.ID, t.Titulo, ag, t.ListoCuando)
		}
		return s, nil
	}
	if err := spec.SaveFeatureState(root, s); err != nil {
		return spec.Spec{}, fmt.Errorf("no se pudo guardar la aceptación del feature: %w", err)
	}

	if err := out.Data(applied); err != nil {
		return spec.Spec{}, err
	}

	if esTUI() {
		var cuerpo strings.Builder
		titulo := fmt.Sprintf("APLICADAS %d TAREAS", len(applied))
		if s.Feature != "" {
			titulo += " (" + s.Feature + ")"
		}
		cuerpo.WriteString(tui.Titulo(titulo))
		cuerpo.WriteString("\n\n")
		for _, t := range applied {
			ag := ""
			if t.Agente != "" {
				ag = " [" + t.Agente + "]"
			}
			cuerpo.WriteString(t.ID)
			cuerpo.WriteString("  ")
			cuerpo.WriteString(t.Titulo)
			cuerpo.WriteString(ag)
			cuerpo.WriteString("  ")
			cuerpo.WriteString(tui.Apagado("· listo cuando: " + t.ListoCuando))
			cuerpo.WriteString("\n")
		}
		out.Line("%s", tui.Caja(strings.TrimRight(cuerpo.String(), "\n")))
	} else {
		out.Line("✓ %d tareas aplicadas desde %s:", len(applied), filepath.Base(filePath))
		for _, t := range applied {
			ag := ""
			if t.Agente != "" {
				ag = " [" + t.Agente + "]"
			}
			out.Line("  %s  %s%s  · listo cuando: %s", t.ID, t.Titulo, ag, t.ListoCuando)
		}
	}

	if runImmediately {
		out.Line("")
		agentes := s.Agentes
		if agentes < 1 {
			agentes = 1
		}
		return s, runCmd(agentes, "", "", false, false)
	}

	out.Line("\n· corre loom run (o loom up) para ejecutarlas en paralelo")
	return s, nil
}
