package main

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
)

const reglaOroListoCuando = "listo_cuando debe ser un comando que hoy falla y que el agente hará pasar."

func imprimirPlantillasListoCuando(stack string) {
	ejemplos := config.PlantillasListoCuando(stack)
	if len(ejemplos) == 0 {
		out.Line("· %s", reglaOroListoCuando)
		return
	}
	out.Line(idioma.T("· listo_cuando debe fallar hoy · ejemplos para %s:"), stack)
	for _, e := range ejemplos {
		out.Line("  %s", e)
	}
}

func notasListoCuando(stack string) string {
	ejemplos := config.PlantillasListoCuando(stack)
	if len(ejemplos) == 0 {
		return reglaOroListoCuando
	}
	var b strings.Builder
	fmt.Fprintf(&b, "listo_cuando debe ser un comando que hoy falla. Ejemplos para %s:\n", stack)
	for _, e := range ejemplos {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	return strings.TrimRight(b.String(), "\n")
}
