package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/tui"
)

type psItem struct {
	ID     string `json:"id"`
	Titulo string `json:"titulo"`
	Estado string `json:"estado"`
	Agente string `json:"agente,omitempty"`
	Puerto int    `json:"puerto,omitempty"`
	Rama   string `json:"rama,omitempty"`
	Error  string `json:"error,omitempty"`
}

func newPsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ps",
		Short: "muestra el estado de tareas y cuartos activos (estilo compose)",
		Long: `Lista todas las tareas del proyecto con su estado actual (pendiente, en_curso,
lista, detenida), agente asignado y cuarto/puerto si está activa.`,
		Example: `  loom ps
  loom ps --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runPs(root)
		},
	}
}

func runPs(root string) error {
	tasksDir := config.TasksDir(root)
	tasks, err := task.List(tasksDir)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		out.Line("sin tareas · crea una con loom plan, loom apply o loom task add")
		return nil
	}

	estados, err := state.List(root)
	if err != nil {
		return err
	}
	mapaEstados := map[string]state.State{}
	for _, st := range estados {
		mapaEstados[st.ID] = st
	}

	var items []psItem
	for _, t := range tasks {
		st, ok := mapaEstados[t.ID]
		estado := state.Pendiente
		var puerto int
		var rama string
		var ultErr string
		if ok {
			estado = st.Estado
			puerto = st.Puerto
			rama = st.Rama
			ultErr = st.UltimoError
		}
		items = append(items, psItem{
			ID:     t.ID,
			Titulo: t.Titulo,
			Estado: estado,
			Agente: t.Agente,
			Puerto: puerto,
			Rama:   rama,
			Error:  ultErr,
		})
	}

	if err := out.Data(items); err != nil {
		return err
	}

	if esTUI() {
		var b strings.Builder
		b.WriteString(tui.Titulo("ESTADO DE TAREAS Y CUARTOS"))
		b.WriteString("\n\n")
		for _, it := range items {
			colorEstado := it.Estado
			switch it.Estado {
			case state.Lista:
				colorEstado = tui.Presion(it.Estado)
			case state.EnCurso:
				colorEstado = tui.Espera(it.Estado)
			case state.Detenida:
				colorEstado = tui.Alerta(it.Estado)
			default:
				colorEstado = tui.Apagado(it.Estado)
			}
			ag := ""
			if it.Agente != "" {
				ag = " [" + it.Agente + "]"
			}
			extra := ""
			if it.Puerto > 0 {
				extra = fmt.Sprintf(" · :%d", it.Puerto)
			}
			if it.Error != "" {
				extra += " (" + it.Error + ")"
			}
			b.WriteString(fmt.Sprintf("%-6s %-10s %s%s%s\n", it.ID, colorEstado, it.Titulo, ag, tui.Apagado(extra)))
		}
		out.Line("%s", tui.Caja(strings.TrimRight(b.String(), "\n")))
	} else {
		for _, it := range items {
			ag := ""
			if it.Agente != "" {
				ag = " [" + it.Agente + "]"
			}
			extra := ""
			if it.Puerto > 0 {
				extra = fmt.Sprintf(" · puerto %d", it.Puerto)
			}
			if it.Error != "" {
				extra += fmt.Sprintf(" · error: %s", it.Error)
			}
			out.Line("%s  %-10s  %s%s%s", it.ID, it.Estado, it.Titulo, ag, extra)
		}
	}
	return nil
}
