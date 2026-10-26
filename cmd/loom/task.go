package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "manejo manual de tareas",
	}
	cmd.AddCommand(
		newTaskAddCmd(),
		newTaskEditCmd(),
		newTaskRmCmd(),
		newTaskListCmd(),
		newTaskCheckCmd(),
		newTaskSealCmd(),
	)
	return cmd
}

// projectRoot resuelve la raíz del repo y exige que .loom exista.
func projectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := config.RepoRoot(cwd)
	if err != nil {
		return "", err
	}
	if !config.Exists(root) {
		return "", errors.New(idioma.T("sin configuración · corre loom init primero"))
	}
	return root, nil
}

func validTaskID(id string) error {
	if !task.ValidID(id) {
		return fmt.Errorf(idioma.T("id inválido: %s · usa el formato T-001"), id)
	}
	return nil
}

func newTaskAddCmd() *cobra.Command {
	var agente string
	cmd := &cobra.Command{
		Use:   `add "<título>"`,
		Short: "crea una tarea nueva con id correlativo",
		Example: `  loom task add "exportar clientes a CSV"
  loom task add "diseñar arquitectura" --agente architect`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			titulo := strings.TrimSpace(args[0])
			if titulo == "" {
				return errors.New(idioma.T("falta el título · loom task add \"lo que necesitas\""))
			}
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runTaskAdd(root, titulo, agente)
		},
	}
	cmd.Flags().StringVar(&agente, "agent", "", "agente asignado para la tarea (declarado en config.yml)")
	return cmd
}

func runTaskAdd(root, titulo, agente string) error {
	dir := config.TasksDir(root)
	id, err := task.NextID(dir)
	if err != nil {
		return err
	}
	stack := config.DetectLanguage(root)
	t := task.Task{
		Version:        task.Version,
		ID:             id,
		Titulo:         titulo,
		TocarSolo:      []string{},
		NoTocar:        []string{},
		Agente:         agente,
		LimiteIntentos: task.DefaultLimiteIntentos,
		LimiteLineas:   task.DefaultLimiteLineas,
		Notas:          notasListoCuando(stack),
	}
	if err := task.Save(dir, t); err != nil {
		return err
	}
	if err := out.Data(t); err != nil {
		return err
	}
	out.Line(idioma.T("✓ %s creada · completa listo_cuando con loom task edit %s"), id, id)
	imprimirPlantillasListoCuando(stack)
	return nil
}

func newTaskSealCmd() *cobra.Command {
	var visible, oculta string
	var forzar bool
	cmd := &cobra.Command{
		Use:   "seal <id>",
		Short: "sella una suite oculta escrita a mano, sin gastar modelo",
		Long: `Sella pruebas que escribiste vos —o que generó un modelo caro corriendo
aparte, fuera del bucle del implementador— en vez de dejárselas al
examinador ciego.

La suite --visible se copia al cuarto de la tarea cuando corra: el
implementador la ve y es su criterio de aceptación. La suite --oculta se
sella con hash y solo corre en la esclusa de salida (loom ship), que
no distingue si la escribió un humano o el examinador.

Los archivos se leen del disco al sellar, así que después puedes borrarlos:
lo sellado ya no depende de ellos.`,
		Example: `  loom task seal T-001 --visible pruebas/visible_test.go --oculta pruebas/oculta_test.go
  loom task seal T-001 --visible v.py --oculta o.py --forzar`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := validTaskID(id); err != nil {
				return err
			}
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runTaskSeal(root, id, visible, oculta, forzar)
		},
	}
	cmd.Flags().StringVar(&visible, "visible", "", "archivo con las pruebas que el implementador SÍ ve")
	cmd.Flags().StringVar(&oculta, "hidden", "", "archivo con las pruebas que se sellan y solo corren en ship")
	cmd.Flags().BoolVar(&forzar, "force", false, "reemplaza una suite ya sellada")
	_ = cmd.MarkFlagRequired("visible")
	_ = cmd.MarkFlagRequired("hidden")
	return cmd
}

// runTaskSeal sella una suite escrita a mano. Las rutas dentro del cuarto
// salen de examiner.RutasSuite, el mismo camino que usa el examinador
// automático: de acá para abajo nadie distingue el origen.
func runTaskSeal(root, id, visiblePath, ocultaPath string, forzar bool) error {
	t, err := task.Load(config.TasksDir(root), id)
	if err != nil {
		return fmt.Errorf(idioma.T("no existe la tarea %s · créala con loom task add"), id)
	}
	if sealed.Exists(root, id) && !forzar {
		return fmt.Errorf(idioma.T("%s ya tiene una suite sellada · usa --forzar para reemplazarla"), id)
	}

	visible, err := leerSuite(root, visiblePath)
	if err != nil {
		return err
	}
	oculta, err := leerSuite(root, ocultaPath)
	if err != nil {
		return err
	}

	rutaVisible, rutaOculta := examiner.RutasSuite(t.TocarSolo, config.DetectLanguage(root))
	s := sealed.SuiteOculta{
		Content:        oculta,
		Archivo:        rutaOculta,
		Visible:        visible,
		ArchivoVisible: rutaVisible,
	}
	if err := sealed.Write(root, id, s); err != nil {
		return fmt.Errorf(idioma.T("no se pudo sellar la suite de %s · %s"), id, err)
	}

	if err := out.Data(map[string]string{
		"id":      id,
		"visible": rutaVisible,
		"oculta":  rutaOculta,
	}); err != nil {
		return err
	}
	out.Line(idioma.T("✓ %s sellada a mano · visible → %s en el cuarto"), id, rutaVisible)
	out.Line(idioma.T("  la oculta se verifica en loom ship y se quema al usarse"))
	return nil
}

// leerSuite lee un archivo de pruebas del disco. Las rutas relativas se
// resuelven contra la raíz del repo, no contra el cuarto: sellar pasa
// antes de que el cuarto exista.
func leerSuite(root, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf(idioma.T("no se pudo leer %s · %s"), p, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf(idioma.T("%s está vacío · una suite sin pruebas no verifica nada"), p)
	}
	return string(data), nil
}

func newTaskEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id>",
		Short: "abre la tarea en $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := validTaskID(id); err != nil {
				return err
			}
			root, err := projectRoot()
			if err != nil {
				return err
			}
			path := task.Path(config.TasksDir(root), id)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf(idioma.T("no existe la tarea %s"), id)
			}

			editor := strings.Fields(os.Getenv("EDITOR"))
			if len(editor) == 0 {
				editor = []string{"vi"}
			}
			edit := exec.Command(editor[0], append(editor[1:], path)...)
			edit.Stdin = os.Stdin
			edit.Stdout = os.Stdout
			edit.Stderr = os.Stderr
			if err := edit.Run(); err != nil {
				return fmt.Errorf(idioma.T("el editor falló: %s"), err)
			}

			t, err := task.Load(config.TasksDir(root), id)
			if err != nil {
				return fmt.Errorf(idioma.T("%s quedó mal formada · %s · reábrela con loom task edit %s"), id, err, id)
			}
			if errs := t.Validate(); len(errs) > 0 {
				msgs := make([]string, len(errs))
				for i, e := range errs {
					msgs[i] = e.Error()
				}
				return fmt.Errorf(idioma.T("%s incompleta · %s"), id, strings.Join(msgs, " · "))
			}
			if err := out.Data(t); err != nil {
				return err
			}
			out.Line(idioma.T("✓ %s guardada y válida"), id)
			return nil
		},
	}
}

func newTaskRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "elimina una tarea",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := validTaskID(id); err != nil {
				return err
			}
			root, err := projectRoot()
			if err != nil {
				return err
			}
			dir := config.TasksDir(root)
			// lo que se pierde al borrarla se dice antes: en una corrida real, se
			// borraron la tarea de cableado (verde, sin entregar) y la que
			// escribía la prueba de punta a punta, y la entrega frenó en
			// una aceptación que ya nadie podía cumplir
			var avisos []string
			if t, err := task.Load(dir, id); err == nil {
				if st, err := state.Get(root, id); err == nil && st.Estado == state.Lista && room.RamaExiste(context.Background(), root, id) {
					avisos = append(avisos, "su trabajo verde en "+room.Branch(id)+" no se va a entregar")
				}
				if f, err := spec.LoadFeatureState(root); err == nil {
					for _, c := range f.AcceptanceCommands() {
						if strings.Contains(t.ListoCuando, c) {
							avisos = append(avisos, "la aceptación del feature sigue corriendo `"+c+"`, que cumplía esta tarea: la entrega va a frenar ahí (edita .loom/feature.json o vuelve a crear la tarea)")
						}
					}
				}
			}
			if err := task.Remove(dir, id); err != nil {
				return err
			}
			if err := state.Remove(root, id); err != nil {
				return err
			}
			// las que dependían de ella quedarían bloqueadas para siempre
			// esperando una tarea que ya no existe
			liberadas, err := quitarDependencia(dir, id)
			if err != nil {
				return err
			}
			if err := out.Data(map[string]any{"eliminada": id, "liberadas": liberadas}); err != nil {
				return err
			}
			out.Line(idioma.T("✓ %s eliminada"), id)
			for _, a := range avisos {
				out.Line(idioma.T("· ojo: %s"), a)
			}
			if len(liberadas) > 0 {
				out.Line(idioma.T("· ya no dependen de ella: %s · revisa que sigan teniendo sentido sin %s"), strings.Join(liberadas, ", "), id)
			}
			return nil
		},
	}
}

// quitarDependencia saca id del depende_de de las tareas de dir y
// devuelve cuáles cambió.
func quitarDependencia(dir, id string) ([]string, error) {
	tareas, err := task.List(dir)
	if err != nil {
		return nil, err
	}
	var cambiadas []string
	for _, t := range tareas {
		if !slices.Contains(t.DependeDe, id) {
			continue
		}
		t.DependeDe = slices.DeleteFunc(t.DependeDe, func(d string) bool { return d == id })
		if err := task.Save(dir, t); err != nil {
			return cambiadas, err
		}
		cambiadas = append(cambiadas, t.ID)
	}
	return cambiadas, nil
}

type listEntry struct {
	ID          string   `json:"id"`
	Titulo      string   `json:"titulo"`
	ListoCuando string   `json:"listo_cuando"`
	Estado      string   `json:"estado"`
	Problemas   []string `json:"problemas,omitempty"`
}

func newTaskListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "lista las tareas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			tasks, err := task.List(config.TasksDir(root))
			if err != nil {
				return err
			}

			entries := make([]listEntry, 0, len(tasks))
			for _, t := range tasks {
				entry := listEntry{
					ID:          t.ID,
					Titulo:      t.Titulo,
					ListoCuando: t.ListoCuando,
					Estado:      "válida",
				}
				if errs := t.Validate(); len(errs) > 0 {
					entry.Estado = "incompleta"
					for _, e := range errs {
						entry.Problemas = append(entry.Problemas, e.Error())
					}
				}
				entries = append(entries, entry)
			}

			if err := out.Data(entries); err != nil {
				return err
			}
			if len(entries) == 0 {
				out.Line(idioma.T("sin tareas · empieza con loom task add \"lo que necesitas\""))
				return nil
			}
			for _, e := range entries {
				if e.Estado == "válida" {
					out.Line("✓ %s  %s", e.ID, e.Titulo)
				} else {
					out.Line(idioma.T("· %s  %s · incompleta: %s"), e.ID, e.Titulo, strings.Join(e.Problemas, " · "))
				}
			}
			return nil
		},
	}
}
