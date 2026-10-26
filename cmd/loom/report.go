package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/metrics"
)

// TimeoutFriccion es el techo de lo que report se permite esperar a gh
// para medir la fricción. report es un comando de lectura: más vale
// imprimir "— sin datos" que quedarse colgado contra una red mala.
const TimeoutFriccion = 20 * time.Second

func newReportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "métricas del proyecto",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReport(cmd)
		},
	}
}

func runReport(cmd *cobra.Command) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	d, err := metrics.Recoger(root)
	if err != nil {
		return err
	}
	m := metrics.Calcular(d)

	// la fricción es la única métrica que no sale de los artefactos del
	// repo: el ciclo de revisión pasa en el PR. Se pregunta a gh y, si no
	// se puede, queda en null como estaba.
	ctx, cancel := context.WithTimeout(cmd.Context(), TimeoutFriccion)
	defer cancel()
	m.Friccion = metrics.Friccion(ctx, root, d.Entregas)

	historial, err := metrics.LeerHistorial(root)
	if err != nil {
		return err
	}
	var prev *metrics.Metricas
	if n := len(historial); n > 0 {
		prev = &historial[n-1].Metricas
	}
	tendencia := metrics.Comparar(prev, m)

	if err := metrics.GuardarHistorial(root, m); err != nil {
		return err
	}

	if err := out.Data(metrics.Reporte{Metricas: m, Tendencia: tendencia}); err != nil {
		return err
	}
	out.Line("intentos hasta verde  %s %.1f   (meta ≤ 2)", tendencia.IntentosHastaVerde, m.IntentosHastaVerde)
	out.Line("ruido                 %s %.1f%%  (meta < 5%%)", tendencia.Ruido, m.Ruido)
	out.Line("roce                  %s %.1f por 10 entregas  (meta < 1)", tendencia.Roce, m.Roce)
	if m.Friccion != nil {
		out.Line("fricción              %s %.0f min", tendencia.Friccion, *m.Friccion)
	} else {
		out.Line("fricción              %s —  sin datos", tendencia.Friccion)
	}
	out.Line("rechazo en entrada    %s %.1f%%", tendencia.RechazoEntrada, m.RechazoEntrada)
	out.Line("tokens                %d", m.Tokens)
	return nil
}
