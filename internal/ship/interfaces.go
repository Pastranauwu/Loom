package ship

import (
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

// verificarExpone comprueba que la tarea entregó las firmas que su
// contrato prometía. Una tarea puede pasar sus propias pruebas y
// aun así no exponer lo que su hermana consume: la hermana corre en otro
// cuarto y no se entera hasta que las dos ramas se juntan, cuando ya es
// tarde.
//
// Devuelve las firmas que faltan; vacío si están todas o si la tarea no
// declaró ninguna.
func verificarExpone(firmas []string, diff string) (faltan, noVerificables []string) {
	if len(firmas) == 0 {
		return nil, nil
	}

	var añadido strings.Builder
	var rutas strings.Builder
	for _, ad := range parseDiffAnadido(diff) {
		for _, linea := range ad.lineas {
			añadido.WriteString(linea)
			añadido.WriteByte('\n')
		}
		// las rutas de archivo del diff también cuentan como "lo que la
		// tarea entrega": un expone puede ser un archivo entero (un
		// interaction-model, un README, un servicio systemd), y su nombre
		// vive en el encabezado del diff, no en las líneas añadidas.
		rutas.WriteString(ad.nombre)
		rutas.WriteByte('\n')
	}
	cuerpo := añadido.String()
	cuerpo += rutas.String()

	for _, f := range firmas {
		nombre, verificable := task.FirmaVerificable(f)
		if !verificable {
			// prosa en vez de contrato: no hay nada que buscar en el
			// diff, y frenar por eso deja la tarea sin salida
			noVerificables = append(noVerificables, f)
			continue
		}
		if strings.Contains(cuerpo, nombre) {
			continue
		}
		faltan = append(faltan, f)
	}
	return faltan, noVerificables
}
