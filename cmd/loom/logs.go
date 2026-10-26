package main

import (
	"github.com/Pastranauwu/loom/internal/idioma"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
)

type logsResult struct {
	ID       string         `json:"id"`
	Estado   string         `json:"estado"`
	Intentos []loop.Attempt `json:"intentos"`
}

func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <id>",
		Short: "detalle interno de una tarea",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(args[0])
		},
	}
}

func runLogs(id string) error {
	if err := validTaskID(id); err != nil {
		return err
	}
	root, err := projectRoot()
	if err != nil {
		return err
	}
	st, err := state.Get(root, id)
	if err != nil {
		return err
	}
	attempts, err := loop.ReadAttempts(root, id)
	if err != nil {
		return err
	}

	if err := out.Data(logsResult{ID: id, Estado: st.Estado, Intentos: attempts}); err != nil {
		return err
	}

	out.Line(idioma.T("%s · estado: %s"), id, st.Estado)
	if len(attempts) == 0 {
		out.Line(idioma.T("sin intentos · la tarea todavía no corre"))
		return nil
	}
	for _, a := range attempts {
		code := "—"
		if a.SalidaCodigo != nil {
			code = strconv.Itoa(*a.SalidaCodigo)
		}
		archivos := strings.Join(a.ArchivosTocados, ", ")
		if archivos == "" {
			archivos = "—"
		}
		out.Line(idioma.T("  intento %d  salida %s  +%d/-%d  %s  modelo %s  tokens %d/%d"),
			a.Intento, code, a.LineasMas, a.LineasMenos, archivos, a.Modelo, a.Tokens.Entrada, a.Tokens.Salida)
		if a.ErrorAgente != "" {
			out.Line(idioma.T("    ✗ el agente falló · %s"), a.ErrorAgente)
		}
		if a.Log != "" {
			out.Line(idioma.T("    log: %s"), a.Log)
		}
	}
	if st.Pregunta != "" {
		out.Line("  %s", st.Pregunta)
	}
	return nil
}
