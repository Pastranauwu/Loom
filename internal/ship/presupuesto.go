package ship

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

// ToleranciaPresupuesto es cuánto puede pasarse una tarea de su
// limite_lineas sin que la entrega se frene.
//
// El límite lo estima un modelo antes de que exista una sola línea de
// código, así que un desvío chico es ruido de estimación y no
// descontrol. Frenar ahí sale caro: el trabajo ya está verde y
// verificado, y la única salida era editar el contrato a mano y volver
// a entregar. Un desbordamiento de verdad —una tarea que se volvió
// tres— sigue frenando la compuerta.
const ToleranciaPresupuesto = 1.5

// verificarPresupuesto acota el gasto en diff: las líneas añadidas no pueden
// pasar de limite_lineas más la tolerancia; los archivos tocados se
// reportan.
func verificarPresupuesto(mas, menos, masPrueba, archivos int, t task.Task) (string, bool) {
	limite := t.LimiteLineas
	if limite == 0 {
		return fmt.Sprintf(idioma.T("%d líneas de código · %d líneas de prueba · %d archivos · sin límite de líneas"), mas-masPrueba, masPrueba, archivos), true
	}
	// el límite es sobre el código de la solución: las pruebas se
	// reportan aparte porque en go y python no las escribe el agente
	codigo := mas - masPrueba
	detalle := fmt.Sprintf(idioma.T("%d líneas de %d · %d archivos"), codigo, limite, archivos)
	if masPrueba > 0 {
		detalle += fmt.Sprintf(idioma.T(" · %d líneas de prueba aparte"), masPrueba)
	}
	techo := int(float64(limite) * ToleranciaPresupuesto)
	switch {
	case codigo > techo:
		// decir el número exacto ahorra el ida y vuelta: es el que hay
		// que escribir si el cambio es correcto y el límite era corto
		return fmt.Sprintf(idioma.T("presupuesto excedido · %s · reduce el cambio, o sube limite_lineas a %d en .loom/tasks/%s.md"),
			detalle, codigo, t.ID), false
	case codigo > limite:
		return detalle + " · sobre lo estimado, dentro de la tolerancia", true
	}
	return detalle, true
}

// patronesPruebaDe devuelve los patrones de ruta de prueba efectivos:
// los del proyecto, o los de fábrica si la config no los declara.
func patronesPruebaDe(c config.Config) []string {
	if len(c.PatronesPrueba) > 0 {
		return c.PatronesPrueba
	}
	return config.DefaultTestPatterns()
}
